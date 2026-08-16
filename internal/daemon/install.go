package daemon

type InstallState struct {
	Installed bool
	Running   bool
	Path      string
	Detail    string
}

func Install() error {
	bin, err := Binary()
	if err != nil {
		return err
	}
	return install(bin)
}

func Uninstall() error {
	return uninstall()
}

func Query() (InstallState, error) {
	return query()
}
