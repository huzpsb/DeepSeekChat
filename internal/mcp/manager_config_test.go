package mcp

import (
	"errors"
	"sync"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

func TestManager_Config_ReturnsCopy(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{Provider: "p1", Model: "m1"})

	mgr := NewManager()
	if err := mgr.LoadAndConnect(); err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}

	cfg := mgr.Config()
	if cfg.Provider != "p1" || cfg.Model != "m1" {
		t.Errorf("unexpected config: %+v", cfg)
	}

	// Mutating the copy must not affect the live config.
	cfg.Provider = "hacked"
	if mgr.Config().Provider != "p1" {
		t.Errorf("live config was mutated through the copy")
	}
}

func TestManager_Config_NonNilBeforeLoad(t *testing.T) {
	setupManagerTest(t)
	mgr := NewManager()
	// Must not panic even if LoadAndConnect was never called (or failed).
	_ = mgr.Config()
}

func TestManager_UpdateConfig_Persists(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	if err := mgr.LoadAndConnect(); err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}

	err := mgr.UpdateConfig(func(cfg *model.MCPConfig) error {
		cfg.Provider = "p2"
		cfg.Model = "m2"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	if got := mgr.Config().Provider; got != "p2" {
		t.Errorf("in-memory config not updated, got %q", got)
	}
	disk, err := storage.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if disk.Provider != "p2" || disk.Model != "m2" {
		t.Errorf("config not persisted, got %+v", disk)
	}
}

func TestManager_UpdateConfig_ValidationErrorSavesNothing(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{Provider: "orig"})

	mgr := NewManager()
	if err := mgr.LoadAndConnect(); err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}

	sentinel := errors.New("invalid")
	err := mgr.UpdateConfig(func(cfg *model.MCPConfig) error {
		return sentinel // reject without touching anything
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	if got := mgr.Config().Provider; got != "orig" {
		t.Errorf("config changed despite rejection: %q", got)
	}
	disk, _ := storage.LoadConfig()
	if disk.Provider != "orig" {
		t.Errorf("disk config overwritten despite rejection: %q", disk.Provider)
	}
}

// TestManager_ConfigConcurrentAccess exercises Config/UpdateConfig against
// Reload and the tool-list readers. Run with -race.
func TestManager_ConfigConcurrentAccess(t *testing.T) {
	setupManagerTest(t)
	seedConfig(&model.MCPConfig{})

	mgr := NewManager()
	if err := mgr.LoadAndConnect(); err != nil {
		t.Fatalf("LoadAndConnect failed: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Readers: Config, GetTools, GetAllowedTools.
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = mgr.Config()
					_ = mgr.GetTools()
					_ = mgr.GetAllowedTools()
				}
			}
		}()
	}

	// Writer: UpdateConfig.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = mgr.UpdateConfig(func(cfg *model.MCPConfig) error {
				cfg.Model = "m"
				return nil
			})
		}
	}()

	// Reload swaps m.config concurrently.
	for i := 0; i < 5; i++ {
		if err := mgr.Reload(); err != nil {
			t.Errorf("Reload failed: %v", err)
		}
	}

	close(stop)
	wg.Wait()
}
