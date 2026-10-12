// Package guard holds the check that every test package in the module runs
// its tests under testinputs.Main, so that no package's cached test result
// silently omits what its subprocesses read. It has no code of its own.
package guard
