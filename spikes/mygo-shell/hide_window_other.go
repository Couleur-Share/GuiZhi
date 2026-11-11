//go:build !windows

package main

import "os/exec"

func hideChildWindow(*exec.Cmd) {}
