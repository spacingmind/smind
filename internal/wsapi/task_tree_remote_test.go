package wsapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/spacingmind/smind/internal/store"
)

// remoteTreeNode is a partial decode of a task.tree node with the F4 remote
// fields (treeNode stays as it was).
type remoteTreeNode struct {
	treeNode
	Host     string `json:"host"`
	HostID   string `json:"hostId"`
	Remote   bool   `json:"remote"`
	GlobalID string `json:"globalId"`
}

type remoteTreeResult struct {
	Nodes []remoteTreeNode `json:"nodes"`
}

type remoteTreeFixture struct {
	ws     *websocket.Conn
	db     *store.Store
	w      workspace1
	hostID string
}

const remoteMirrorCost = 0.5

func newRemoteTreeFixture(t *testing.T) *remoteTreeFixture {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	runner := newTestRunner(wm)
	srv := newTestWSServer(t, wm, runner, db, "tok")
	ws := dialWS(t, srv, "tok")
	repo := newTestRepo(t)
	sendRequest(t, ws, "ws", "workspace.create", map[string]any{"path": repo, "title": "W", "routingPolicy": "hard"})
	resp := readEnvelopeFor(t, ws, "ws", 5*time.Second)
	if resp.Error != nil {
		t.Fatalf("workspace.create error = %v", resp.Error.Message)
	}
	var w struct {
		ID int64 `json:"ID"`
	}
	if err := json.Unmarshal(resp.Result, &w); err != nil {
		t.Fatal(err)
	}
	create := func(id string, params map[string]any) int64 {
		sendRequest(t, ws, id, "task.create", params)
		resp := readEnvelopeFor(t, ws, id, 5*time.Second)
		if resp.Error != nil {
			t.Fatalf("task.create error = %v", resp.Error.Message)
		}
		var task struct {
			ID int64 `json:"ID"`
		}
		if err := json.Unmarshal(resp.Result, &task); err != nil {
			t.Fatal(err)
		}
		return task.ID
	}
	lead := create("t1", map[string]any{"workspaceId": w.ID, "title": "lead"})
	create("t2", map[string]any{"workspaceId": w.ID, "title": "coder", "parentTaskId": lead})

	const hostID = "host-1"
	if err := db.CreateFleetPair(store.FleetHost{HostID: hostID, Alias: "mac", Pub: []byte("pub"), Label: "Mac", State: "paired"}, nil); err != nil {
		t.Fatalf("CreateFleetPair() error = %v", err)
	}
	cost := remoteMirrorCost
	if _, err := db.PutFleetRemoteTaskAndWatch(store.FleetRemoteTask{
		HostID: hostID, RemoteTaskID: "7", LocalTaskID: lead, Title: "remote coder", State: "running", CostUSD: &cost,
	}, nil); err != nil {
		t.Fatalf("PutFleetRemoteTaskAndWatch() error = %v", err)
	}
	return &remoteTreeFixture{ws: ws, db: db, w: workspace1{ID: w.ID, Lead: lead}, hostID: hostID}
}

func setFleetEnabled(t *testing.T, on bool) {
	t.Helper()
	old := configFleetEnabled
	t.Cleanup(func() { configFleetEnabled = old })
	configFleetEnabled = func() bool { return on }
}

func (f *remoteTreeFixture) tree(t *testing.T) []remoteTreeNode {
	t.Helper()
	sendRequest(t, f.ws, "rt", "task.tree", map[string]any{"workspaceId": f.w.ID})
	resp := readEnvelopeFor(t, f.ws, "rt", 10*time.Second)
	if resp.Error != nil {
		t.Fatalf("task.tree error = %v", resp.Error.Message)
	}
	var r remoteTreeResult
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		t.Fatal(err)
	}
	return r.Nodes
}

func localNodesJSON(t *testing.T, nodes []remoteTreeNode) string {
	t.Helper()
	var local []treeNode
	for _, n := range nodes {
		if !n.Remote {
			local = append(local, n.treeNode)
		}
	}
	b, err := json.Marshal(local)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func findRemote(nodes []remoteTreeNode) *remoteTreeNode {
	for i := range nodes {
		if nodes[i].Remote {
			return &nodes[i]
		}
	}
	return nil
}

func TestTaskTreeHandlerRemoteOnlyWhenFleetOn(t *testing.T) {
	f := newRemoteTreeFixture(t)

	setFleetEnabled(t, true)
	nodes := f.tree(t)
	r := findRemote(nodes)
	if r == nil {
		t.Fatalf("fleet on: no remote node in %+v", nodes)
	}
	if r.Host != "mac" || r.HostID != f.hostID || r.GlobalID != "mac:7" {
		t.Fatalf("remote node host=%q hostId=%q globalId=%q, want mac/%s/mac:7", r.Host, r.HostID, r.GlobalID, f.hostID)
	}
	if r.ID >= 0 || r.ParentID == nil || *r.ParentID != f.w.Lead {
		t.Fatalf("remote node id=%d parent=%v, want negative id under lead %d", r.ID, r.ParentID, f.w.Lead)
	}
	if r.Title != "remote coder" || r.Run.State != "running" {
		t.Fatalf("remote node title=%q state=%q", r.Title, r.Run.State)
	}
	if len(nodes) != 3 {
		t.Fatalf("fleet on: %d nodes, want 2 local + 1 remote", len(nodes))
	}

	configFleetEnabled = func() bool { return false }
	nodes = f.tree(t)
	if findRemote(nodes) != nil || len(nodes) != 2 {
		t.Fatalf("fleet off: nodes = %+v, want exactly the 2 local nodes", nodes)
	}
}

func TestMirrorRowsAreCacheOnly(t *testing.T) {
	f := newRemoteTreeFixture(t)
	setFleetEnabled(t, true)

	before := f.tree(t)
	if findRemote(before) == nil {
		t.Fatal("no remote node before")
	}
	rootCost := func(nodes []remoteTreeNode) float64 {
		for _, n := range nodes {
			if n.ID == f.w.Lead && n.Usage.Subt.CostUSD != nil {
				return *n.Usage.Subt.CostUSD
			}
		}
		return 0
	}
	withMirror := rootCost(before)
	if withMirror != remoteMirrorCost {
		t.Fatalf("lead subtree cost = %v, want %v", withMirror, remoteMirrorCost)
	}

	for i, method := range []string{"task.stopTree", "task.archiveFinished"} {
		id := "act" + string(rune('a'+i))
		sendRequest(t, f.ws, id, method, map[string]any{"taskId": f.w.Lead})
		resp := readEnvelopeFor(t, f.ws, id, 10*time.Second)
		if resp.Error != nil {
			t.Fatalf("%s error = %v", method, resp.Error.Message)
		}
		if s := string(resp.Result); strings.Contains(s, "remote coder") || strings.Contains(s, "mac:7") || strings.Contains(s, f.hostID) {
			t.Fatalf("%s result mentions the mirror: %s", method, s)
		}
	}
	rows, err := f.db.ListFleetRemoteTasks("")
	if err != nil || len(rows) != 1 || rows[0].State != "running" {
		t.Fatalf("mirror rows after actions = %+v, %v; want the one untouched row", rows, err)
	}

	if err := f.db.DeleteFleetRemoteTask(f.hostID, "7"); err != nil {
		t.Fatal(err)
	}
	after := f.tree(t)
	if findRemote(after) != nil {
		t.Fatal("remote node still shown after the mirror was deleted")
	}
	// The local nodes after the delete are exactly the local tree with fleet off.
	configFleetEnabled = func() bool { return false }
	if a, b := localNodesJSON(t, after), localNodesJSON(t, f.tree(t)); a != b {
		t.Fatalf("local nodes after delete differ from the fleet-off tree:\n%s\n%s", a, b)
	}
	configFleetEnabled = func() bool { return true }
	if got := rootCost(after); withMirror-got != remoteMirrorCost {
		t.Fatalf("lead subtree cost dropped by %v, want %v", withMirror-got, remoteMirrorCost)
	}
	if len(after) != 2 || after[0].ID != before[0].ID || after[1].ID != before[1].ID {
		t.Fatalf("local node identity changed: before=%+v after=%+v", before, after)
	}
}

func TestAliasRenameKeepsWireIds(t *testing.T) {
	f := newRemoteTreeFixture(t)
	setFleetEnabled(t, true)

	if err := f.db.UpdateFleetHost(f.hostID, "studio", "Mac", "paired"); err != nil {
		t.Fatalf("UpdateFleetHost() error = %v", err)
	}
	r := findRemote(f.tree(t))
	if r == nil {
		t.Fatal("no remote node after rename")
	}
	if r.Host != "studio" || r.GlobalID != "studio:7" || r.HostID != f.hostID {
		t.Fatalf("after rename host=%q globalId=%q hostId=%q, want studio/studio:7/%s", r.Host, r.GlobalID, r.HostID, f.hostID)
	}
	rows, err := f.db.ListFleetRemoteTasks("")
	if err != nil || len(rows) != 1 || rows[0].HostID != f.hostID || rows[0].RemoteTaskID != "7" {
		t.Fatalf("mirror rows after rename = %+v, %v; want host_id %s remote_task_id 7", rows, err, f.hostID)
	}
}
