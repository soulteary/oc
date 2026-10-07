package cmd

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConfigLoadCacheConcurrent(t *testing.T) {
	previousDir := mcCustomConfigDir
	cfgMutex.Lock()
	previousCache := cacheCfgV10
	cacheCfgV10 = nil
	cfgMutex.Unlock()
	defer func() {
		mcCustomConfigDir = previousDir
		cfgMutex.Lock()
		cacheCfgV10 = previousCache
		cfgMutex.Unlock()
	}()
	mcCustomConfigDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(mcCustomConfigDir, "config.json"), []byte(`{"version":"10","aliases":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	configs := make([]*configV10, 16)
	var wg sync.WaitGroup
	for i := range configs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			cfg, err := loadConfigV10()
			if err != nil {
				t.Error(err)
				return
			}
			configs[i] = cfg
		}(i)
	}
	close(start)
	wg.Wait()
	for _, cfg := range configs {
		if cfg == nil || cfg != configs[0] {
			t.Fatal("concurrent loads did not share one cached configuration")
		}
	}
}
