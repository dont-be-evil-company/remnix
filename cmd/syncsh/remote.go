package main

import (
	"github.com/spf13/cobra"
)

func newSyncCmd() *cobra.Command {
	var endpoint string
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Synchronize encrypted history with configured endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd, endpoint)
		},
	}
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "sync only this endpoint")
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show local and remote synchronization status",
		RunE:  runSyncStatus,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Diagnose synchronization state (read-only)",
		RunE:  runSyncDoctor,
	})
	return cmd
}

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Interactive setup for device identity, transport, and encryption",
		RunE:  runSetup,
	}
}

func newDeviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "device",
		Short: "Manage synchronized devices",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List known devices",
		RunE:  runDeviceList,
	})
	add := &cobra.Command{
		Use:   "add",
		Short: "Register this device with an existing remote",
		RunE:  runDeviceAdd,
	}
	add.Flags().String("name", "", "device display name")
	cmd.AddCommand(add)
	retire := &cobra.Command{
		Use:   "retire <device-id>",
		Short: "Retire a device so it no longer blocks garbage collection",
		Args:  cobra.ExactArgs(1),
		RunE:  runDeviceRetire,
	}
	cmd.AddCommand(retire)
	prune := &cobra.Command{
		Use:   "prune <device-id>",
		Short: "Permanently remove a device from the roster",
		Long:  "Delete the device from the local roster and remote metadata. Synced history from that machine is kept. Later syncs will not bring the device back. Use retire to only stop it blocking GC.",
		Args:  cobra.ExactArgs(1),
		RunE:  runDevicePrune,
	}
	cmd.AddCommand(prune)
	return cmd
}

func newKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage encryption credentials and key generations",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show active generation and key slots",
		RunE:  runKeyStatus,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "generation",
		Short: "List key generations",
		RunE:  runKeyGenerationList,
	})
	yk := &cobra.Command{Use: "yubikey", Short: "Manage YubiKey PIV slots"}
	yk.AddCommand(&cobra.Command{Use: "add", Short: "Add a YubiKey slot wrapping the current SMK", RunE: runKeyYubiKeyAdd})
	yk.AddCommand(&cobra.Command{Use: "remove", Short: "Remove a YubiKey slot after another unlock method is verified", RunE: runKeyYubiKeyRemove})
	cmd.AddCommand(yk)
	fido := &cobra.Command{Use: "fido", Short: "Manage FIDO2 hmac-secret slots"}
	fido.AddCommand(&cobra.Command{Use: "add", Short: "Enroll a FIDO2 security key wrapping the current SMK over USB HID", RunE: runKeyFIDOAdd})
	fido.AddCommand(&cobra.Command{Use: "remove", Short: "Remove a FIDO2 slot after another unlock method is verified", RunE: runKeyFIDORemove})
	cmd.AddCommand(fido)
	recovery := &cobra.Command{Use: "recovery", Short: "Recovery key commands"}
	recovery.AddCommand(&cobra.Command{
		Use:   "rotate",
		Short: "Replace the software recovery key without re-encrypting history",
		RunE:  runKeyRecoveryRotate,
	})
	cmd.AddCommand(recovery)
	cmd.AddCommand(&cobra.Command{
		Use:   "rotate",
		Short: "Create a new Sync Master Key generation",
		RunE:  runKeyRotate,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "recover [generation-id]",
		Short: "Adopt a remote key generation and restore history from its checkpoint",
		Long:  "Replace a local forked generation with one already on the remote (defaults to the generation named by the newest checkpoint), unlock it, republish metadata/manifest, and load the checkpoint snapshot.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runKeyRecover,
	})
	return cmd
}

func newGCCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "gc",
		Short: "Garbage-collect remote objects that are acknowledged and superseded",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGC(cmd, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "report eligible objects without deleting")
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show garbage-collection eligibility",
		RunE:  runGCStatus,
	})
	return cmd
}

func newDatabaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "database",
		Short: "Local SQLite database operations",
	}
	cmd.AddCommand(&cobra.Command{Use: "migrate", Short: "Apply pending migrations", RunE: runDatabaseMigrate})
	cmd.AddCommand(&cobra.Command{Use: "status", Short: "Show migration status", RunE: runDatabaseStatus})
	cmd.AddCommand(&cobra.Command{Use: "doctor", Short: "Check database integrity and migrations", RunE: runDatabaseDoctor})
	return cmd
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run read-only diagnostics for config, database, keys, and remote",
		RunE:  runDoctor,
	}
}

func newUnlockCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlock",
		Short: "Unlock with recovery key or FIDO2 and store the SMK in the OS keyring",
		RunE:  runUnlock,
	}
}

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the login-time history sync daemon",
		RunE:  runDaemon,
	}
	cmd.AddCommand(&cobra.Command{Use: "install", Short: "Install user-session autostart for the sync daemon", RunE: runDaemonInstall})
	cmd.AddCommand(&cobra.Command{Use: "uninstall", Short: "Remove login autostart for the sync daemon", RunE: runDaemonUninstall})
	cmd.AddCommand(&cobra.Command{Use: "status", Short: "Show daemon install and last sync status", RunE: runDaemonStatus})
	return cmd
}
