package wsclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDialURLSendsHeaderAndServes(t *testing.T) {
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t1" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		var req envelope
		if err := ws.ReadJSON(&req); err != nil {
			return
		}
		_ = ws.WriteJSON(envelope{ID: req.ID, Result: json.RawMessage(`{"ok":true}`)})
		_, _, _ = ws.ReadMessage()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := DialURL(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", http.Header{"Authorization": {"Bearer t1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var out struct{ OK bool }
	if err := c.Call(ctx, "x", nil, &out); err != nil || !out.OK {
		t.Fatalf("call: %v %+v", err, out)
	}
}
