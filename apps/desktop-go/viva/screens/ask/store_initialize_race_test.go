package ask

import (
	"encoding/json"
	"sync"
	"testing"
)

// gatedPrefs 在 memPrefs 上加两个一次性的卡点，用来把「首次加载」与「新建会话」卡成确定的交错：
//   - 下一次 Set 进入时通知 setEntered，并卡到 releaseSet 关闭（落地前的 remember）；
//   - 下一次 Get 读到值之后通知 getRead，并卡到 releaseGet 关闭（首次加载读到旧的记住会话之后）。
type gatedPrefs struct {
	*memPrefs

	mu         sync.Mutex
	setEntered chan struct{}
	releaseSet chan struct{}
	getRead    chan struct{}
	releaseGet chan struct{}
}

func (g *gatedPrefs) takeSet() (entered, release chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	entered, release = g.setEntered, g.releaseSet
	g.setEntered, g.releaseSet = nil, nil
	return entered, release
}

func (g *gatedPrefs) takeGet() (read, release chan struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()
	read, release = g.getRead, g.releaseGet
	g.getRead, g.releaseGet = nil, nil
	return read, release
}

func (g *gatedPrefs) Set(key string, value any) error {
	if entered, release := g.takeSet(); entered != nil {
		close(entered)
		<-release
	}
	return g.memPrefs.Set(key, value)
}

func (g *gatedPrefs) Get(key string) (json.RawMessage, bool) {
	raw, ok := g.memPrefs.Get(key)
	if read, release := g.takeGet(); read != nil {
		close(read)
		<-release
	}
	return raw, ok
}

// 「就此提问」可能发生在问答模块首次出现之前：新建会话与首次加载同时在后台跑。
// 交错：新建会话已把代次加一、还没落地（卡在记住新会话）；这时首次加载开始，拿到加一之后的代次并读到旧的记住会话；
// 新建会话落地后首次加载才返回——它不能把新建的空会话盖回上次的会话。
// （root_ask_test.go 里 TestRootAskAboutItemJumpsWithDraft 约 4% 偶发失败的根因。）
func TestInitializeDoesNotOverwriteSessionCreatedDuringLoad(t *testing.T) {
	r := newRig(t)
	gp := &gatedPrefs{memPrefs: r.prefs}
	r.store.Prefs = gp
	r.putAsk("s1", done("m1", "q1", "a1"))
	if err := r.prefs.Set(PrefActiveSession, "s1"); err != nil {
		t.Fatal(err)
	}

	setEntered, releaseSet := make(chan struct{}), make(chan struct{})
	getRead, releaseGet := make(chan struct{}), make(chan struct{})
	gp.setEntered, gp.releaseSet = setEntered, releaseSet

	// 1. 新建会话：代次加一，卡在记住新会话（还没落地）。
	waitNew := goAsync(r.store.NewSession)
	<-setEntered

	// 2. 首次加载：取到加一之后的代次，读到旧的记住会话 s1 后卡住。
	gp.mu.Lock()
	gp.getRead, gp.releaseGet = getRead, releaseGet
	gp.mu.Unlock()
	waitInit := goAsync(r.store.Initialize)
	<-getRead

	// 3. 新建会话落地。
	close(releaseSet)
	waitNew()
	fresh := r.view().ActiveSessionID
	if fresh == "" || fresh == "s1" {
		t.Fatalf("新建会话应已落地为新的空会话：%+v", r.view())
	}

	// 4. 首次加载返回：只补历史列表，活跃会话与消息保持新建的空会话。
	close(releaseGet)
	waitInit()
	v := r.view()
	if v.ActiveSessionID != fresh || len(v.Messages) != 0 {
		t.Fatalf("首次加载不能把新建的空会话盖回上次会话：active=%q 消息=%d（期望 %q 且 0 条）", v.ActiveSessionID, len(v.Messages), fresh)
	}
	if !v.HasLoaded || v.LoadError != "" {
		t.Fatalf("首次加载仍应放行界面：%+v", v)
	}
	if len(v.Sessions) != 1 || v.Sessions[0].ID != "s1" {
		t.Fatalf("历史列表仍应补上：%+v", v.Sessions)
	}
	if got := r.prefs.active(); got != fresh {
		t.Fatalf("记住的会话应是新建的那个：%q", got)
	}
}
