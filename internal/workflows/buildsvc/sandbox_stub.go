//go:build !linux

package buildsvc

func getSandboxInjector() sandboxInjector {
	return nil
}
