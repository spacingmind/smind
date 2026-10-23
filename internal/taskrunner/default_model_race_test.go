package taskrunner

import (
	"sync"
	"testing"
)

func TestRunner_DefaultModelConcurrent(t *testing.T) {
	r := New(nil)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if m := r.DefaultModel(ProviderGLM); m != "" && m != "a" && m != "b" {
					t.Errorf("torn model %q", m)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = r.ProviderCatalog()
			}
		}()
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				switch (i + j) % 3 {
				case 0:
					r.SetDefaultModel(ProviderGLM, "a")
				case 1:
					r.SetDefaultModel(ProviderGLM, "b")
				default:
					r.SetDefaultModel(ProviderGLM, "")
				}
			}
		}(i)
	}
	wg.Wait()
}
