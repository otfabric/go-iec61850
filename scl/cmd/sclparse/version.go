package main

import "fmt"

func versionString(name string) string {
	s := fmt.Sprintf("%s %s", name, version)
	if tag != "" {
		s += fmt.Sprintf(" (%s)", tag)
	}
	if commit != "" {
		short := commit
		if len(short) > 8 {
			short = short[:8]
		}
		s += fmt.Sprintf(" commit %s", short)
	}
	if buildDate != "" {
		s += fmt.Sprintf(" built %s", buildDate)
	}
	return s
}

func versionTemplate(name string) string {
	return versionString(name) + "\n"
}
