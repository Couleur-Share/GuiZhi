package main

// 对应 packages/db/src/knowledge-draft.ts 与 shared/types/knowledge-draft.ts 的 mergeDraftTags：
// 编辑器自动保存草稿——带基线比较，冲突时不覆盖，标签按相对基线的增删合并。
//
// 已知差异：TS 在一个事务里先读再写；这里读与写之间有一个很小的窗口。
// 目前只有渲染进程这一条写入路径、写入走 writeMu 串行，窗口内没有别的写者。

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
)

// objectKeys 对应 Object.keys(v)：数组/字符串给下标，数字与布尔没有自有键。
func objectKeys(v any) []string {
	switch x := v.(type) {
	case *jsObj:
		return x.keys()
	case jsArr:
		keys := make([]string, len(x))
		for i := range x {
			keys[i] = strconv.Itoa(i)
		}
		return keys
	case string:
		n := utf16Len(x)
		keys := make([]string, n)
		for i := 0; i < n; i++ {
			keys[i] = strconv.Itoa(i)
		}
		return keys
	}
	return nil
}

// mergeDraftTags 对应同名 TS 函数：以 trim + 小写作键，Map 语义（首次出现的位置、最后写入的值）。
func mergeDraftTags(base, edited, current []string) []string {
	key := func(s string) string { return jsLower(jsTrim(s)) }
	wanted := map[string]bool{}
	for _, n := range edited {
		wanted[key(n)] = true
	}
	removed := map[string]bool{}
	for _, n := range base {
		if !wanted[key(n)] {
			removed[key(n)] = true
		}
	}
	var order []string
	result := map[string]string{}
	set := func(k, v string) {
		if _, ok := result[k]; !ok {
			order = append(order, k)
		}
		result[k] = v
	}
	for _, n := range current {
		if !removed[key(n)] {
			set(key(n), n)
		}
	}
	original := map[string]bool{}
	for _, n := range base {
		original[key(n)] = true
	}
	for _, n := range edited {
		if !original[key(n)] {
			set(key(n), n)
		}
	}
	out := make([]string, 0, len(order))
	for _, k := range order {
		out = append(out, result[k])
	}
	return out
}

func stringsOf(v any) []string {
	arr, _ := v.(jsArr)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, e.(string))
	}
	return out
}

func (l *Library) knowledgeSaveDraft(ctx context.Context, input any) (any, error) {
	fail := func(msg string) (any, error) { return map[string]any{"ok": false, "error": msg}, nil }
	id, ok1 := jsGet(input, "id").(string)
	_, ok2 := jsGet(input, "requestId").(string)
	base, patch := jsGet(input, "base"), jsGet(input, "patch")
	if !vTruthy(input) || !ok1 || !ok2 || !vTruthy(patch) || !vTruthy(base) {
		return fail("草稿保存参数无效")
	}
	for _, fields := range []any{base, patch} {
		for _, k := range objectKeys(fields) {
			if k != "title" && k != "content" && k != "tagNames" {
				return fail("草稿字段无效")
			}
		}
		for _, k := range []string{"title", "content"} {
			if x := jsGet(fields, k); x != jsUndefined {
				if _, ok := x.(string); !ok {
					return fail("草稿字段无效")
				}
			}
		}
		if tn := jsGet(fields, "tagNames"); tn != jsUndefined {
			arr, ok := tn.(jsArr)
			if !ok {
				return fail("草稿字段无效")
			}
			for _, e := range arr {
				if _, ok := e.(string); !ok {
					return fail("草稿字段无效")
				}
			}
		}
	}

	cur, err := l.knowledgeGet(ctx, id)
	if err != nil {
		return nil, err
	}
	current, _ := cur.(map[string]any)
	if current == nil || current["deletedAt"] != nil {
		return fail("条目不存在或已移到回收站")
	}
	var conflicts []any
	for _, field := range []string{"title", "content"} {
		p := jsGet(patch, field)
		if p != jsUndefined && !jsStrictEqual(jsGet(base, field), current[field]) && !jsStrictEqual(p, current[field]) {
			conflicts = append(conflicts, field)
		}
	}
	if len(conflicts) > 0 {
		return map[string]any{"ok": false, "item": current, "conflicts": conflicts, "error": "该内容已在其他操作中修改，请选择要保留的版本"}, nil
	}

	// patch 的展开副本，只含 title / content / tagNames
	out := newObj()
	spreadInto(out, patch)
	if tn := jsGet(patch, "tagNames"); tn != jsUndefined {
		var currentNames []string
		for _, t := range current["tags"].([]map[string]any) {
			currentNames = append(currentNames, t["name"].(string))
		}
		var baseNames []string
		if bt := jsGet(base, "tagNames"); bt != jsUndefined {
			baseNames = stringsOf(bt)
		}
		merged := mergeDraftTags(baseNames, stringsOf(tn), currentNames)
		arr := make(jsArr, len(merged))
		for i, s := range merged {
			arr[i] = s
		}
		out.set("tagNames", arr)
	}
	raw, _ := jsStringify(out)
	var in UpdateInput
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &in); err != nil {
		return nil, err
	}
	item, err := l.knowledgeUpdate(ctx, id, in)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return fail("条目不存在")
	}
	return map[string]any{"ok": true, "item": item}, nil
}

func (l *Library) registerKnowledgeDraft(b *Bridge) {
	b.handlers["knowledge:saveDraft"] = func(ctx context.Context, a []json.RawMessage) (any, error) {
		return l.knowledgeSaveDraft(ctx, jsArg(a, 0))
	}
}
