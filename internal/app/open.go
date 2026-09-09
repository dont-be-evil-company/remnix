package app

import (
	"fmt"
	"os"

	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/dont-be-evil-company/remnix/internal/db"
	"github.com/dont-be-evil-company/remnix/internal/device"
)

type App struct {
	Config *config.Config
	DB     *db.DB
}

func Open() (*App, error) {
	return OpenOpts(OpenOptions{})
}

type OpenOptions struct {
	SkipMigrate bool
}

func OpenOpts(opts OpenOptions) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(config.DataDir(), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	d, err := db.Open(cfg.Database.Path)
	if err != nil {
		return nil, err
	}
	skip := opts.SkipMigrate || cfg.DisableAutoMigrate
	if !skip {
		if err := db.Migrate(d.SQL); err != nil {
			_ = d.Close()
			return nil, fmt.Errorf("migrate: %w", err)
		}
	}
	return &App{Config: cfg, DB: d}, nil
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	return a.DB.Close()
}

func (a *App) EnsureLocalDevice() error {
	if a.Config.DeviceID == "" {
		id, err := device.NewID()
		if err != nil {
			return err
		}
		a.Config.EnsureDevice(id, device.DefaultName())
		if err := a.Config.Save(); err != nil {
			return err
		}
	}
	store := device.NewStore(a.DB)
	_, found, err := store.Get(a.Config.DeviceID)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	host, _ := os.Hostname()
	return store.Upsert(device.Device{
		ID:       a.Config.DeviceID,
		Name:     a.Config.DeviceName,
		Hostname: host,
		Status:   device.StatusActive,
	})
}
