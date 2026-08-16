package providers

type Category string

const (
	CategoryCloud    Category = "cloud"
	CategoryNetwork  Category = "network"
	CategoryAdvanced Category = "advanced"
)

type AuthKind string

const (
	AuthOAuth    AuthKind = "oauth"
	AuthKeys     AuthKind = "keys"
	AuthPassword AuthKind = "password"
	AuthEnv      AuthKind = "env"
	AuthMixed    AuthKind = "mixed"
)

type Definition struct {
	ID          string
	DisplayName string
	RcloneType  string
	Description string
	Category    Category
	AuthKind    AuthKind
	Featured    bool
	Limitations []string
	Reconnect   bool
}

func All() []Definition {
	return []Definition{
		{ID: "s3", DisplayName: "AWS S3 / S3-compatible", RcloneType: "s3", Description: "Amazon S3 and compatible object storage", Category: CategoryCloud, AuthKind: AuthKeys, Featured: true, Limitations: []string{"directories are prefixes; rename can be expensive", "scoped IAM cannot list all buckets; enter the bucket name"}},
		{ID: "gcs", DisplayName: "Google Cloud Storage", RcloneType: "gcs", Description: "Google Cloud Storage buckets", Category: CategoryCloud, AuthKind: AuthMixed, Featured: true, Limitations: []string{"directories are prefixes; rename can be expensive", "identities without storage.buckets.list must enter the bucket name"}},
		{ID: "dropbox", DisplayName: "Dropbox", RcloneType: "dropbox", Description: "Dropbox via OAuth", Category: CategoryCloud, AuthKind: AuthOAuth, Featured: true},
		{ID: "azure-files", DisplayName: "Microsoft Azure Files", RcloneType: "azurefiles", Description: "Azure Files shares", Category: CategoryCloud, AuthKind: AuthKeys, Featured: true},
		{ID: "icloud-drive", DisplayName: "iCloud Drive", RcloneType: "iclouddrive", Description: "iCloud Drive (Apple ID + 2FA)", Category: CategoryCloud, AuthKind: AuthPassword, Featured: true, Reconnect: true, Limitations: []string{"authentication may expire and require reconnection", "interactive 2FA is required to reconnect"}},
		{ID: "onedrive", DisplayName: "Microsoft OneDrive", RcloneType: "onedrive", Description: "OneDrive via OAuth", Category: CategoryCloud, AuthKind: AuthOAuth, Featured: true},
		{ID: "google-drive", DisplayName: "Google Drive", RcloneType: "drive", Description: "Google Drive via OAuth", Category: CategoryCloud, AuthKind: AuthOAuth, Featured: true},
		{ID: "webdav", DisplayName: "WebDAV", RcloneType: "webdav", Description: "Nextcloud, ownCloud, SharePoint, generic WebDAV", Category: CategoryNetwork, AuthKind: AuthPassword, Featured: true, Limitations: []string{"capabilities vary by server/vendor"}},
		{ID: "smb", DisplayName: "SMB / CIFS", RcloneType: "smb", Description: "Windows/Samba file shares", Category: CategoryNetwork, AuthKind: AuthPassword, Featured: true, Limitations: []string{"a share must be selected"}},
		{ID: "custom", DisplayName: "Generic rclone backend", RcloneType: "", Description: "Any backend compiled into this remnix binary", Category: CategoryAdvanced, AuthKind: AuthMixed, Featured: false},
	}
}

func Featured() []Definition {
	var out []Definition
	for _, d := range All() {
		if d.Featured {
			out = append(out, d)
		}
	}
	return out
}

func ByID(id string) (Definition, bool) {
	for _, d := range All() {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}

func ByRcloneType(t string) (Definition, bool) {
	for _, d := range All() {
		if d.RcloneType != "" && d.RcloneType == t {
			return d, true
		}
	}
	return Definition{}, false
}
