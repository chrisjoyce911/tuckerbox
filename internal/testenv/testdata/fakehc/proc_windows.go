package main

import "errors"

func startHost(name string) (int, error) {
	return 0, errors.New("fake terminals are Unix only")
}

func host(name string) { fail(2, "fakehc: fake terminals are Unix only") }

func alive(pid int) bool { return false }

func hangup(pid int) {}

func killGroup(pid int) {}
