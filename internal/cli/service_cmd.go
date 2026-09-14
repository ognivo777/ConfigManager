package cli

import (
	"fmt"

	"github.com/example/cm/internal/service"
)

// cmdInstall installs the operating-system service.
func cmdInstall(args []string) error {
	repo := ""
	if len(args) > 1 {
		return fmt.Errorf("usage: cm --install [/path/to/repository]")
	}
	if len(args) == 1 {
		repo = args[0]
	}
	if err := service.Install(repo); err != nil {
		return err
	}
	fmt.Println("cm service installed")
	return nil
}

// cmdUninstall removes the operating-system service.
func cmdUninstall() error {
	if err := service.Uninstall(); err != nil {
		return err
	}
	fmt.Println("cm service uninstalled")
	return nil
}

// serviceControl forwards start/stop to the service manager.
func serviceControl(action string) int {
	if err := service.Control(action); err != nil {
		return fail(err)
	}
	fmt.Printf("cm service %sed\n", action)
	return 0
}
