//go:build !linux

package main

func getSandboxInjector() sandboxInjector {
	return nil
}
