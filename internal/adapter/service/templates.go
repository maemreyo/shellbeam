// Package service renders and manages per-user daemon definitions.
package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
)

func SystemdUnit(executable, config string) (string, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(config) {
		return "", fmt.Errorf("paths must be absolute")
	}
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }
	return "[Unit]\nDescription=ShellBeam local shell daemon\n\n[Service]\nType=simple\nExecStart=" + quote(executable) + " daemon --config " + quote(config) + "\nRestart=on-failure\nRestartSec=1\nUMask=0077\n\n[Install]\nWantedBy=default.target\n", nil
}

type plist struct {
	XMLName xml.Name `xml:"plist"`
	Version string   `xml:"version,attr"`
	Dict    dict     `xml:"dict"`
}
type dict struct {
	Content string `xml:",innerxml"`
}

func LaunchdPlist(executable, config string) (string, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(config) {
		return "", fmt.Errorf("paths must be absolute")
	}
	escape := func(v string) string { var b bytes.Buffer; template.HTMLEscape(&b, []byte(v)); return b.String() }
	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>com.shellbeam.daemon</string>
<key>ProgramArguments</key><array><string>` + escape(executable) + `</string><string>daemon</string><string>--config</string><string>` + escape(config) + `</string></array>
<key>KeepAlive</key><true/><key>ProcessType</key><string>Background</string><key>Umask</key><integer>63</integer>
</dict></plist>
`
	return body, nil
}
