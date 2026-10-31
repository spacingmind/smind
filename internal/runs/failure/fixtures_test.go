package failure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixture is one recorded or documented error payload and the classification
// it must get. See testdata/<harness>/*.json; "source" says where the payload
// comes from (recorded, or documented: <reference>).
type fixture struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Input  struct {
		Provider          string `json:"provider"`
		Vendor            string `json:"vendor"`
		Err               string `json:"err"`
		Code              string `json:"code"`
		RetryAfterSeconds int    `json:"retryAfterSeconds"`
		Model             string `json:"model"`
		Signal            *struct {
			Status   string    `json:"status"`
			Type     string    `json:"type"`
			ResetsAt time.Time `json:"resetsAt"`
		} `json:"signal"`
	} `json:"input"`
	Now  time.Time `json:"now"`
	Want struct {
		Kind              string `json:"kind"`
		Window            string `json:"window"`
		ResetsAt          string `json:"resetsAt"`
		RetryAfterSeconds int    `json:"retryAfterSeconds"`
	} `json:"want"`
}

// TestClassify_Fixtures runs every testdata/<harness>/*.json through Classify:
// adding a payload is adding one file. No live provider call is ever made.
func TestClassify_Fixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures under testdata/<harness>/*.json")
	}
	for _, path := range files {
		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var f fixture
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			if f.Source == "" {
				t.Fatalf("%s: a fixture names its source (recorded | documented: <ref>)", path)
			}
			in := Input{
				Provider: f.Input.Provider, Vendor: f.Input.Vendor, Err: f.Input.Err, Code: f.Input.Code, Model: f.Input.Model,
				RetryAfter: time.Duration(f.Input.RetryAfterSeconds) * time.Second,
			}
			if s := f.Input.Signal; s != nil {
				in.Signal = &Signal{Status: s.Status, Type: s.Type, ResetsAt: s.ResetsAt}
			}
			got := Classify(in, f.Now)
			reset := ""
			if got.ResetAt != nil {
				reset = got.ResetAt.UTC().Format(time.RFC3339)
			}
			if got.Kind != f.Want.Kind || got.Window != f.Want.Window || reset != f.Want.ResetsAt || int(got.RetryAfter.Seconds()) != f.Want.RetryAfterSeconds {
				t.Fatalf("%s: got kind=%q window=%q resetsAt=%q retryAfter=%v, want kind=%q window=%q resetsAt=%q retryAfter=%ds",
					f.Name, got.Kind, got.Window, reset, got.RetryAfter, f.Want.Kind, f.Want.Window, f.Want.ResetsAt, f.Want.RetryAfterSeconds)
			}
		})
	}
}
