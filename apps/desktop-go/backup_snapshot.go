package main

// 备份快照内容收集，对应 backup-repository.ts 的 collectSnapshotEntries 与 backup-repository-themed-reading.ts。

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type collectedEntries struct {
	entries       []snapshotEntry
	itemCount     int
	schemaVersion int
	configDomains []string
	reused        int
	created       int
}

// themedBackupAsset 对应 ThemedReadingBackupAsset。
type themedBackupAsset struct {
	fileName string
	sha256   string
	bytes    *int64
}

func tableExists(db *sql.DB, name string) bool {
	var n string
	return db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&n) == nil
}

// inspectThemedReadingBackup 取出备份里主题阅读页引用的图片资源。
// 与 TS 的差异（待主题阅读一族迁移时补齐）：TS 还会逐版本过一遍 themedReadingDocument 的 HTML 清理与全文槽位校验，
// 并核对「版本→任务」的归属；这里只核对资源索引表自身的一致性（同一文件的 sha256/bytes 不得互相矛盾）。
func inspectThemedReadingBackup(db *sql.DB) ([]themedBackupAsset, error) {
	if !tableExists(db, "themed_reading_assets") {
		return nil, nil
	}
	rows, err := db.Query("SELECT file_name, sha256, bytes FROM themed_reading_assets ORDER BY version_id, file_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := map[string]themedBackupAsset{}
	var order []string
	for rows.Next() {
		var name, sum string
		var size int64
		if err := rows.Scan(&name, &sum, &size); err != nil {
			return nil, err
		}
		if name == "" {
			continue
		}
		if prev, ok := files[name]; ok {
			if prev.sha256 != "" && sum != "" && (prev.sha256 != sum || *prev.bytes != size) {
				return nil, errors.New("主题阅读页共享资源校验信息不一致")
			}
			if prev.sha256 != "" {
				continue
			}
		} else {
			order = append(order, name)
		}
		b := size
		files[name] = themedBackupAsset{fileName: name, sha256: sum, bytes: &b}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]themedBackupAsset, 0, len(order))
	for _, n := range order {
		out = append(out, files[n])
	}
	return out, nil
}

// verifyThemedReadingBackupAssets 文件必须来自指定的图片目录，不能通过备份载荷提供磁盘路径。
func verifyThemedReadingBackupAssets(assets []themedBackupAsset, imagesDir string) error {
	for _, a := range assets {
		file := filepath.Join(imagesDir, a.fileName)
		info, err := os.Lstat(file)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || (a.bytes != nil && info.Size() != *a.bytes) {
			return fmt.Errorf("主题阅读页图片长度无效: %s", a.fileName)
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		if a.sha256 != "" && hex.EncodeToString(sum[:]) != a.sha256 {
			return fmt.Errorf("主题阅读页图片校验失败: %s", a.fileName)
		}
	}
	return nil
}

// assetNames 按首次出现顺序收集集合。
type orderedSet struct {
	seen  map[string]bool
	order []string
}

func newOrderedSet() *orderedSet { return &orderedSet{seen: map[string]bool{}} }
func (s *orderedSet) add(v string) {
	if !s.seen[v] {
		s.seen[v] = true
		s.order = append(s.order, v)
	}
}

// snapshotAssetNames 收集一份数据库快照引用的全部媒体文件名（图片/视频），顺序与 TS 一致。
func snapshotAssetNames(db *sql.DB) (images, videos *orderedSet, themed []themedBackupAsset, err error) {
	images, videos = newOrderedSet(), newOrderedSet()
	contents, err := queryStrings(db, "SELECT content FROM knowledge_items UNION ALL SELECT content FROM source_capture_revisions")
	if err != nil {
		return
	}
	for _, content := range contents {
		for _, n := range extractLocalAssetRefs(content, "image") {
			images.add(n)
		}
		for _, n := range extractLocalAssetRefs(content, "video") {
			videos.add(n)
		}
	}
	versions, err := queryStrings(db, "SELECT payload FROM web_source_versions")
	if err != nil {
		return
	}
	for _, payload := range versions {
		var v struct {
			Markdown string `json:"markdown"`
			Snapshot *struct {
				Assets []struct {
					FileName string `json:"fileName"`
				} `json:"assets"`
			} `json:"snapshot"`
		}
		if err = json.Unmarshal([]byte(payload), &v); err != nil {
			return
		}
		for _, n := range extractLocalAssetRefs(v.Markdown, "image") {
			images.add(n)
		}
		if v.Snapshot != nil {
			for _, a := range v.Snapshot.Assets {
				if !wechatAsset.MatchString(a.FileName) {
					err = errors.New("快照资源路径无效")
					return
				}
				images.add(a.FileName)
			}
		}
	}
	pages, err := queryStrings(db, "SELECT payload FROM crawl_pages")
	if err != nil {
		return
	}
	for _, payload := range pages {
		var p struct {
			Result *struct {
				Snapshot *struct {
					Assets []struct {
						FileName string `json:"fileName"`
					} `json:"assets"`
				} `json:"snapshot"`
			} `json:"result"`
		}
		if err = json.Unmarshal([]byte(payload), &p); err != nil {
			return
		}
		if p.Result != nil && p.Result.Snapshot != nil {
			for _, a := range p.Result.Snapshot.Assets {
				if !wechatAsset.MatchString(a.FileName) {
					err = errors.New("快照资源路径无效")
					return
				}
				images.add(a.FileName)
			}
		}
	}
	themed, err = inspectThemedReadingBackup(db)
	if err != nil {
		return
	}
	for _, a := range themed {
		images.add(a.fileName)
	}
	return
}

func (r *backupRepository) collectEntries(snapDB string, rendererSettings *omap, key []byte) (*collectedEntries, error) {
	c := &collectedEntries{configDomains: []string{}}
	add := func(logical, category string, plain []byte) error {
		comp := "gzip"
		if category == "media" {
			comp = "none"
		}
		e, reused, err := r.storeObject(plain, comp, key)
		if err != nil {
			return err
		}
		e.LogicalPath, e.Category = logical, category
		c.entries = append(c.entries, e)
		if reused {
			c.reused++
		} else {
			c.created++
		}
		return nil
	}

	db, err := openReadOnly(snapDB)
	if err != nil {
		return nil, err
	}
	func() {
		defer db.Close()
		ok, qerr := quickCheckOK(db)
		if qerr != nil || !ok {
			err = errors.New("数据库快照 quick_check 未通过")
			return
		}
		if c.schemaVersion, err = userVersion(db); err != nil {
			return
		}
		if c.schemaVersion > r.schemaVersion {
			err = fmt.Errorf("数据库结构 v%d 高于当前支持的 v%d", c.schemaVersion, r.schemaVersion)
			return
		}
		if err = db.QueryRow("SELECT COUNT(*) FROM knowledge_items").Scan(&c.itemCount); err != nil {
			return
		}
		images, videos, themed, aerr := snapshotAssetNames(db)
		if aerr != nil {
			err = aerr
			return
		}
		type asset struct{ name, folder, path string }
		var assets []asset
		var leased []string
		for _, n := range images.order {
			assets = append(assets, asset{n, "images", filepath.Join(r.paths.ImagesDir, n)})
			leased = append(leased, n)
		}
		for _, n := range videos.order {
			assets = append(assets, asset{n, "videos", filepath.Join(r.paths.VideosDir, n)})
			leased = append(leased, n)
		}
		// 备份期间保护这些文件不被孤儿清理删掉
		release, lerr := retainAssetFiles(leased)
		if lerr != nil {
			err = lerr
			return
		}
		defer release()
		if err = verifyThemedReadingBackupAssets(themed, r.paths.ImagesDir); err != nil {
			return
		}
		for _, a := range assets {
			st, serr := os.Stat(a.path)
			if serr != nil || !st.Mode().IsRegular() {
				err = fmt.Errorf("引用媒体不存在，完整备份已中止: %s", a.name)
				return
			}
			data, rerr := os.ReadFile(a.path)
			if rerr != nil {
				err = rerr
				return
			}
			if err = add("data/assets/"+a.folder+"/"+a.name, "media", data); err != nil {
				return
			}
		}
	}()
	if err != nil {
		return nil, err
	}

	dbBytes, err := os.ReadFile(snapDB)
	if err != nil {
		return nil, err
	}
	if err := add("data/knowledge.db", "database", dbBytes); err != nil {
		return nil, err
	}
	for _, name := range configFileNames {
		p := filepath.Join(r.paths.ConfigDir, name)
		st, serr := os.Stat(p)
		if serr != nil || !st.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		if err := add("config/"+name, "config", data); err != nil {
			return nil, err
		}
		c.configDomains = append(c.configDomains, strings.TrimSuffix(name, ".json"))
	}
	if rendererSettings != nil {
		data, err := marshalNoEscape(rendererSettings)
		if err != nil {
			return nil, err
		}
		if err := add("config/renderer-settings.json", "renderer-settings", data); err != nil {
			return nil, err
		}
		c.configDomains = append(c.configDomains, "interface-preferences")
	}
	return c, nil
}
