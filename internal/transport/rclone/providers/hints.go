package providers

// Intro returns wizard copy shown before the rclone config state machine.
func (d Definition) Intro() string {
	switch d.ID {
	case "s3":
		return "Amazon S3 and compatible stores (MinIO, R2, B2, Wasabi). Directories are prefixes; rename can copy every object. You will be asked for a bucket; listing every bucket in the account is not required."
	case "gcs":
		return "Google Cloud Storage. Prefer Application Default Credentials on this machine, or a service-account JSON file (the path is stored in the local rclone.conf, not in config.yaml). You will be asked for a bucket."
	case "dropbox":
		return "Dropbox via OAuth. A browser window will open; if it does not, paste the URL shown in the wizard."
	case "azure-files":
		return "Azure Files. Use an account key, SAS URL, connection string, or Azure identity. Credentials stay in the local rclone.conf."
	case "icloud-drive":
		return "iCloud Drive uses your Apple ID and may prompt for 2FA. Authentication can expire; reconnect later with: syncsh remote reconnect <name>"
	case "onedrive":
		return "Microsoft OneDrive via OAuth. A browser window will open for sign-in."
	case "google-drive":
		return "Google Drive via OAuth. A browser window will open for sign-in. This is Drive files, not Cloud Storage buckets."
	case "webdav":
		return "WebDAV (Nextcloud, ownCloud, SharePoint, generic). Prefer HTTPS; capabilities vary by server."
	case "smb":
		return "SMB/CIFS share. You must select a share. Guest access is available when the server allows it."
	case "custom":
		return "Any backend compiled into this syncsh binary. Use this for providers without a first-class wizard. Saving after a failed connection test is allowed only here."
	default:
		return d.Description
	}
}

func (d Definition) AllowSaveOnFailedTest() bool {
	return d.ID == "custom"
}
