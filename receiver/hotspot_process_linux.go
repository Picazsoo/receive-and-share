package main

import (
	"os/exec"
	"syscall"
)

func hotspotChildOptions(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
