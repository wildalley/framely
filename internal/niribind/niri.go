// Package niribind prepares a reviewable niri configuration change. It never
// writes anything until the desktop has obtained the user's confirmation.
package niribind

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/wildalley/framely/internal/brand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"text/scanner"
	"time"
	"unicode"

	"github.com/wildalley/framely/internal/shortcutkey"
)

const marker = "// ScreenPin: managed screenshot shortcut\n"

// Current reports a niri session, without starting or changing its services.
func Current() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	for _, name := range strings.Split(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), ":") {
		if name == "niri" {
			return true
		}
	}
	return os.Getenv("XDG_CURRENT_DESKTOP") == "" && os.Getenv("NIRI_SOCKET") != ""
}

type snapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

// Plan captures the files the user is about to approve. Apply rejects edits
// made while the confirmation dialog was open.
type Plan struct {
	ConfigPath     string
	ShortcutPath   string
	Key            string
	ScrollKey      string
	RecordKey      string
	Command        string
	root, shortcut snapshot
	base, content  []byte
	validate       func(context.Context, string) error
}

func activeConfigPath() (string, error) {
	path := os.Getenv("NIRI_CONFIG")
	// niri's command-line option takes precedence over NIRI_CONFIG.
	pid := regexp.MustCompile(`\.(\d+)\.sock$`).FindStringSubmatch(os.Getenv("NIRI_SOCKET"))
	if len(pid) == 2 {
		if data, err := os.ReadFile("/proc/" + pid[1] + "/cmdline"); err == nil {
			args := strings.Split(string(data), "\x00")
			for i, arg := range args {
				if (arg == "-c" || arg == "--config") && i+1 < len(args) {
					path = args[i+1]
				}
				if strings.HasPrefix(arg, "--config=") {
					path = strings.TrimPrefix(arg, "--config=")
				}
			}
		}
	}
	if path != "" {
		if !filepath.IsAbs(path) {
			return "", errors.New("niri 使用相对配置路径，请先将其改为绝对路径")
		}
		return path, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "niri", "config.kdl"), nil
}

// Prepare reads and validates the proposed key and executable, without writing.
func Prepare(key string) (*Plan, error) { return PrepareShortcuts(key, "") }

func PrepareShortcuts(key, scroll string, record ...string) (*Plan, error) {
	path, err := activeConfigPath()
	if err != nil {
		return nil, err
	}
	command, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return prepareShortcuts(path, command, key, scroll, validate, record...)
}

func prepare(path, command, key string, validator func(context.Context, string) error) (*Plan, error) {
	return prepareShortcuts(path, command, key, "", validator)
}

func prepareShortcuts(path, command, key, scroll string, validator func(context.Context, string) error, recording ...string) (*Plan, error) {
	record := ""
	if len(recording) > 0 {
		record = recording[0]
	}
	parsed, err := shortcutkey.Parse(key, "linux")
	if err != nil {
		return nil, err
	}
	root, err := readSnapshot(path, false)
	if err != nil {
		return nil, fmt.Errorf("无法读取 niri 配置: %w", err)
	}
	owned := filepath.Join(filepath.Dir(path), "screenpin-shortcut.kdl")
	old, err := readSnapshot(owned, true)
	if err != nil {
		return nil, err
	}
	if old.exists && !bytes.HasPrefix(old.data, []byte(marker)) {
		return nil, errors.New("screenpin-shortcut.kdl 已存在且不由 " + brand.Name + " 管理，请先为它改名")
	}
	base := bytes.ReplaceAll(root.data, []byte(marker+"include "+strconv.Quote(owned)+"\n"), nil)
	modKey, err := compositorMod(path, base, map[string]bool{})
	if err != nil {
		return nil, err
	}
	if modKey == "" {
		modKey = "Super"
	}
	content := marker + "binds {\n"
	for _, action := range []struct{ key, command string }{{key, "--capture"}, {scroll, "--scroll"}, {record, "--record"}} {
		if action.key == "" {
			continue
		}
		parsed, err := shortcutkey.Parse(action.key, "linux")
		if err != nil {
			return nil, err
		}
		keyName := strings.NewReplacer("LOGO", "Super", "CTRL", "Ctrl", "ALT", "Alt", "SHIFT", "Shift").Replace(parsed.XDGTrigger())
		parts := strings.Split(keyName, "+")
		if len(parts[len(parts)-1]) == 1 {
			parts[len(parts)-1] = strings.ToUpper(parts[len(parts)-1])
			keyName = strings.Join(parts, "+")
		}
		bind := func(name string) string {
			return "    " + strconv.Quote(name) + " repeat=false { spawn " + strconv.Quote(command) + " " + strconv.Quote(action.command) + "; }\n"
		}
		content += bind(keyName)
		if (modKey == "Super" && parsed.Super) || (modKey == "Alt" && parsed.Alt) {
			alias := strings.Split(keyName, "+")
			for i := 0; i < len(alias)-1; i++ {
				if alias[i] == modKey {
					alias[i] = "Mod"
				}
			}
			content += bind(strings.Join(alias, "+"))
		}
	}
	content += "}\n"
	return &Plan{ConfigPath: path, ShortcutPath: owned, Key: parsed.Canonical(), ScrollKey: scroll, RecordKey: record, Command: command,
		root: root, shortcut: old, base: base, content: []byte(content), validate: validator}, nil
}

func (p *Plan) main(include string) []byte {
	out := bytes.Clone(p.base)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return append(out, []byte(marker+"include "+strconv.Quote(include)+"\n")...)
}

// Matches only recognizes the exact managed configuration for this key and
// binary. It does not claim that an arbitrary user's binding is registered.
func Matches(key string) bool { return MatchesShortcuts(key, "") }

func MatchesShortcuts(key, scroll string, record ...string) bool {
	if !Current() {
		return false
	}
	p, err := PrepareShortcuts(key, scroll, record...)
	return err == nil && p.shortcut.exists && bytes.Equal(p.shortcut.data, p.content) && bytes.Equal(p.root.data, p.main(p.ShortcutPath))
}

// Verify checks the installed config with niri before reporting it as usable.
func Verify(ctx context.Context) error {
	path, err := activeConfigPath()
	if err != nil {
		return err
	}
	return validate(ctx, path)
}

// Installed recognizes an owned include, even when a new key is being recorded.
func Installed() bool {
	path, err := activeConfigPath()
	if err != nil {
		return false
	}
	root, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	owned := filepath.Join(filepath.Dir(path), "screenpin-shortcut.kdl")
	data, err := os.ReadFile(owned)
	return err == nil && bytes.HasPrefix(data, []byte(marker)) && bytes.Contains(root, []byte(marker+"include "+strconv.Quote(owned)+"\n"))
}

// Apply validates a staged include and complete config before touching the live
// files. Persistence failures and cancellation restore the previous binding.
func (p *Plan) Apply(ctx context.Context, persist func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.unchanged() {
		return errors.New("niri 配置已变化，请重新确认")
	}
	include, err := tempFile(filepath.Dir(p.ConfigPath), p.content, 0o600)
	if err != nil {
		return err
	}
	defer os.Remove(include)
	staged, err := tempFile(filepath.Dir(p.ConfigPath), p.main(include), p.root.mode)
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	if err := p.validate(ctx, staged); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.unchanged() {
		return errors.New("niri 配置已变化，请重新确认")
	}
	backup, err := os.MkdirTemp(filepath.Dir(p.ConfigPath), "screenpin-backup-"+time.Now().Format("20060102-150405")+"-")
	if err != nil {
		return fmt.Errorf("无法备份 niri 配置: %w", err)
	}
	if err := os.WriteFile(filepath.Join(backup, "config.kdl"), p.root.data, 0o600); err != nil {
		return err
	}
	if p.shortcut.exists {
		if err := os.WriteFile(filepath.Join(backup, "screenpin-shortcut.kdl"), p.shortcut.data, 0o600); err != nil {
			return err
		}
	}
	if err := atomicWrite(p.ShortcutPath, p.content, 0o600); err != nil {
		return err
	}
	main := p.main(p.ShortcutPath)
	if err := atomicWrite(p.ConfigPath, main, p.root.mode); err != nil {
		return errors.Join(err, p.shortcut.restoreIf(p.content))
	}
	err = ctx.Err()
	if err == nil {
		err = persist()
	}
	if err != nil {
		return errors.Join(err, p.root.restoreIf(main), p.shortcut.restoreIf(p.content))
	}
	return nil
}

func (p *Plan) unchanged() bool {
	if !p.root.unchanged() || !p.shortcut.unchanged() {
		return false
	}
	// A shared include may change the Mod key while the dialog is open.
	current, err := prepare(p.ConfigPath, p.Command, p.Key, p.validate)
	return err == nil && bytes.Equal(current.content, p.content)
}

func validate(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "niri", "validate", "-c", path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("niri 配置校验失败，未修改现有绑定: %w\n%s", err, out)
	}
	return nil
}

func readSnapshot(path string, optional bool) (snapshot, error) {
	s := snapshot{path: path}
	info, err := os.Lstat(path)
	if optional && errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if !info.Mode().IsRegular() {
		return s, fmt.Errorf("请手工处理非普通配置文件: %s", path)
	}
	s.data, err = os.ReadFile(path)
	s.mode, s.exists = info.Mode().Perm(), true
	return s, err
}

func (s snapshot) unchanged() bool {
	next, err := readSnapshot(s.path, true)
	return err == nil && s.exists == next.exists && s.mode == next.mode && bytes.Equal(s.data, next.data)
}

func (s snapshot) restoreIf(written []byte) error {
	current, err := os.ReadFile(s.path)
	if err != nil || !bytes.Equal(current, written) {
		return fmt.Errorf("配置在操作期间发生变化，保留该文件，请从备份恢复: %s", s.path)
	}
	if s.exists {
		return atomicWrite(s.path, s.data, s.mode)
	}
	return os.Remove(s.path)
}

func tempFile(dir string, data []byte, mode os.FileMode) (path string, err error) {
	f, err := os.CreateTemp(dir, ".screenpin-*.kdl")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	err = errors.Join(err, f.Close())
	return f.Name(), err
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	temp, err := tempFile(filepath.Dir(path), data, mode)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	return os.Rename(temp, path)
}

// Read literal includes in order to honor an Alt Mod key in shared configs.
// Refuse forms we cannot interpret, instead of configuring the wrong keys.
func compositorMod(path string, data []byte, seen map[string]bool) (string, error) {
	if len(seen) >= 20 || seen[path] {
		return "", errors.New("niri 配置包含循环或过多 include，请手工配置快捷键")
	}
	seen[path] = true
	defer delete(seen, path)
	clean := normalizeKDL(string(data))
	mod := ""
	var s scanner.Scanner
	s.Init(strings.NewReader(clean))
	s.Mode = scanner.ScanIdents | scanner.ScanStrings
	s.Whitespace ^= 1 << '\n'
	s.IsIdentRune = func(r rune, i int) bool {
		return unicode.IsLetter(r) || r == '_' || r == '-' || i > 0 && unicode.IsDigit(r)
	}
	var scanErr error
	s.Error = func(at *scanner.Scanner, message string) {
		scanErr = fmt.Errorf("无法读取 niri 配置，请手工配置快捷键（%s:%d）: %s", path, at.Position.Line, message)
	}
	type frame struct {
		name     string
		disabled bool
	}
	var stack []frame
	parentDisabled := func() bool { return len(stack) > 0 && stack[len(stack)-1].disabled }
	node, argument, disabled, skipValue := "", false, false, false
	for token := s.Scan(); token != scanner.EOF; token = s.Scan() {
		switch token {
		case '\n', ';':
			node, argument, disabled, skipValue = "", false, false, false
			continue
		case '{':
			stack = append(stack, frame{node, disabled || skipValue || parentDisabled()})
			node, argument, disabled, skipValue = "", false, false, false
			continue
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			node, argument, disabled, skipValue = "", false, false, false
			continue
		case '/':
			if s.Peek() != '-' {
				continue
			} // e.g. an unquoted Mod+/ key
			s.Next()
			if node == "" {
				disabled = true
			} else {
				skipValue = true
			}
			continue
		}
		if skipValue {
			skipValue = false
			continue
		}
		if node == "" {
			if token == scanner.Ident {
				node = s.TokenText()
			} else if token == scanner.String {
				node, _ = strconv.Unquote(s.TokenText())
			}
			continue
		}
		if argument || disabled || parentDisabled() {
			continue
		}
		argument = true
		if node == "mod-key" && len(stack) == 1 && stack[0].name == "input" {
			value, err := strconv.Unquote(s.TokenText())
			if err != nil || (value != "Super" && value != "Alt") {
				return "", errors.New("不支持的 niri mod-key，请手工配置快捷键")
			}
			mod = value
		}
		if node != "include" || len(stack) != 0 {
			continue
		}
		if token != scanner.String {
			return "", errors.New("niri include 不是普通字符串，请手工配置快捷键")
		}
		name, err := strconv.Unquote(s.TokenText())
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(name, "~/") {
			home, e := os.UserHomeDir()
			if e != nil {
				return "", e
			}
			name = filepath.Join(home, name[2:])
		}
		if !filepath.IsAbs(name) {
			name = filepath.Join(filepath.Dir(path), name)
		}
		child, err := os.ReadFile(name)
		if err != nil {
			return "", err
		}
		// An include without mod-key should not reset an earlier setting.
		childMod, err := compositorMod(name, child, seen)
		if err != nil {
			return "", err
		}
		if childMod != "" {
			mod = childMod
		}
	}
	return mod, scanErr
}

// Normalize literal raw strings before removing comments, so regexes such as
// r#"[^ "]+//..."# cannot create fake nodes or break string tokenization.
func normalizeKDL(s string) string {
	var out strings.Builder
	quoted, escaped, depth, line := false, false, 0, false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '\n' {
			out.WriteByte(ch)
			line = false
			continue
		}
		if line {
			continue
		}
		if depth > 0 {
			if i+1 < len(s) && s[i:i+2] == "/*" {
				depth++
				i++
			} else if i+1 < len(s) && s[i:i+2] == "*/" {
				depth--
				i++
			}
			continue
		}
		if quoted {
			out.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				quoted = false
			}
			continue
		}
		if ch == 'r' {
			at := i + 1
			for at < len(s) && s[at] == '#' {
				at++
			}
			if at < len(s) && s[at] == '"' {
				end := "\"" + s[i+1:at]
				if n := strings.Index(s[at+1:], end); n >= 0 {
					out.WriteString(strconv.Quote(s[at+1 : at+1+n]))
					i = at + 1 + n + len(end) - 1
					continue
				}
			}
		}
		if ch == '"' {
			quoted = true
			out.WriteByte(ch)
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "//" {
			line = true
			i++
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "/*" {
			depth++
			i++
			out.WriteByte(' ')
			continue
		}
		out.WriteByte(ch)
	}
	return out.String()
}
