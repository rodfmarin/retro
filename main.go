package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const dateLayout = "2006-01-02"

type Config struct {
	VaultPath      string `json:"vault_path"`
	NotesDir       string `json:"notes_dir"`
	CopilotCommand string `json:"copilot_command"`
}

type sectionSpec struct {
	Section string
	Heading string
	Alias   string
}

var sectionSpecs = map[string]sectionSpec{
	"accomplishment": {Section: "accomplishment", Heading: "Accomplishments", Alias: "done"},
	"milestone":      {Section: "milestone", Heading: "Milestones", Alias: "milestone"},
	"wip":            {Section: "wip", Heading: "Work In Progress", Alias: "wip"},
	"info":           {Section: "info", Heading: "Important Information", Alias: "info"},
	"idea":           {Section: "idea", Heading: "Ideas", Alias: "idea"},
	"note":           {Section: "note", Heading: "Notes", Alias: "note"},
}

var sectionOrder = []string{
	"accomplishment",
	"milestone",
	"wip",
	"info",
	"idea",
	"note",
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	command := args[0]
	commandArgs := args[1:]

	switch command {
	case "init":
		return runInit(commandArgs)
	case "capture":
		return runCapture(commandArgs)
	case "done":
		return runQuickCapture("accomplishment", commandArgs)
	case "milestone":
		return runQuickCapture("milestone", commandArgs)
	case "wip":
		return runQuickCapture("wip", commandArgs)
	case "info":
		return runQuickCapture("info", commandArgs)
	case "idea":
		return runQuickCapture("idea", commandArgs)
	case "note":
		return runQuickCapture("note", commandArgs)
	case "today":
		return runToday(commandArgs)
	case "ask":
		return runAsk(commandArgs)
	case "config":
		return runConfig(commandArgs)
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func printUsage() {
	fmt.Println(`retro helps you keep structured daily work notes in an Obsidian vault.

Usage:
	retro init --vault <path> [--notes-dir Worklog] [--copilot-command "copilot -C {vault_path} -p {prompt} --allow-all-tools -s"]
  retro capture --type <accomplishment|milestone|wip|info|idea|note> "text"
  retro done "text"
  retro wip "text"
  retro idea "text"
  retro info "text"
  retro milestone "text"
  retro note "text"
  retro today [--date YYYY-MM-DD] [--print]
  retro ask [--days 7] [--print-only] "question"
  retro config show

Environment:
  RETRO_CONFIG overrides the default config file location.
`)
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	vaultPath := fs.String("vault", "", "Path to your Obsidian vault")
	notesDir := fs.String("notes-dir", "Worklog", "Folder inside the vault where retro writes daily notes")
	copilotCommand := fs.String("copilot-command", `copilot -C {vault_path} -p {prompt} --allow-all-tools -s`, "Command used by 'retro ask'. Use {prompt}, {prompt_file}, or {vault_path} as placeholders.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*vaultPath) == "" {
		return errors.New("--vault is required")
	}

	absoluteVault, err := filepath.Abs(*vaultPath)
	if err != nil {
		return err
	}

	info, err := os.Stat(absoluteVault)
	if err != nil {
		return fmt.Errorf("unable to access vault path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("vault path is not a directory: %s", absoluteVault)
	}

	config := Config{
		VaultPath:      absoluteVault,
		NotesDir:       filepath.Clean(*notesDir),
		CopilotCommand: strings.TrimSpace(*copilotCommand),
	}

	if err := saveConfig(config); err != nil {
		return err
	}

	notePath, err := ensureDailyNote(config, time.Now())
	if err != nil {
		return err
	}

	fmt.Printf("retro configured at %s\n", mustConfigPath())
	fmt.Printf("today's note: %s\n", notePath)
	return nil
}

func runCapture(args []string) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	entryType := fs.String("type", "note", "One of accomplishment, milestone, wip, info, idea, note")
	dateValue := fs.String("date", time.Now().Format(dateLayout), "Date to write to, in YYYY-MM-DD format")
	if err := fs.Parse(args); err != nil {
		return err
	}

	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return errors.New("capture text is required")
	}

	return addEntry(*entryType, *dateValue, text)
}

func runQuickCapture(entryType string, args []string) error {
	fs := flag.NewFlagSet(entryType, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dateValue := fs.String("date", time.Now().Format(dateLayout), "Date to write to, in YYYY-MM-DD format")
	if err := fs.Parse(args); err != nil {
		return err
	}

	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return fmt.Errorf("%s text is required", entryType)
	}

	return addEntry(entryType, *dateValue, text)
}

func addEntry(entryType string, dateValue string, text string) error {
	config, err := loadConfig()
	if err != nil {
		return err
	}

	spec, err := normalizeSection(entryType)
	if err != nil {
		return err
	}

	noteDate, err := time.Parse(dateLayout, dateValue)
	if err != nil {
		return fmt.Errorf("invalid date %q: %w", dateValue, err)
	}

	notePath, err := ensureDailyNote(config, noteDate)
	if err != nil {
		return err
	}

	if err := appendBulletToSection(notePath, spec.Heading, text); err != nil {
		return err
	}

	fmt.Printf("added %s entry to %s\n", spec.Section, notePath)
	return nil
}

func runToday(args []string) error {
	fs := flag.NewFlagSet("today", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dateValue := fs.String("date", time.Now().Format(dateLayout), "Date to show, in YYYY-MM-DD format")
	printContents := fs.Bool("print", false, "Print the note contents")
	if err := fs.Parse(args); err != nil {
		return err
	}

	config, err := loadConfig()
	if err != nil {
		return err
	}

	noteDate, err := time.Parse(dateLayout, *dateValue)
	if err != nil {
		return fmt.Errorf("invalid date %q: %w", *dateValue, err)
	}

	notePath, err := ensureDailyNote(config, noteDate)
	if err != nil {
		return err
	}

	fmt.Println(notePath)
	if *printContents {
		content, readErr := os.ReadFile(notePath)
		if readErr != nil {
			return readErr
		}
		fmt.Print(string(content))
	}
	return nil
}

func runAsk(args []string) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	days := fs.Int("days", 7, "How many days of notes to include")
	printOnly := fs.Bool("print-only", false, "Print the prompt instead of running the configured Copilot command")
	if err := fs.Parse(args); err != nil {
		return err
	}

	question := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if question == "" {
		return errors.New("a question is required")
	}
	if *days <= 0 {
		return errors.New("--days must be greater than zero")
	}

	config, err := loadConfig()
	if err != nil {
		return err
	}

	prompt, files, err := buildPrompt(config, question, *days)
	if err != nil {
		return err
	}

	if *printOnly || strings.TrimSpace(config.CopilotCommand) == "" {
		fmt.Print(prompt)
		return nil
	}

	return runCopilotCommand(config, prompt, files)
}

func runConfig(args []string) error {
	if len(args) == 0 {
		return errors.New("expected a config subcommand, for example: retro config show")
	}

	switch args[0] {
	case "show":
		config, err := loadConfig()
		if err != nil {
			return err
		}
		encoded, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(encoded))
		fmt.Printf("config_path: %s\n", mustConfigPath())
		return nil
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func buildPrompt(config Config, question string, days int) (string, []string, error) {
	files, err := findRecentNotes(config, days)
	if err != nil {
		return "", nil, err
	}

	var builder strings.Builder
	builder.WriteString("You are helping summarize and reason over my work notes.\n")
	builder.WriteString("Read the referenced markdown files directly from disk before answering.\n")
	builder.WriteString("Ignore template prompts, empty sections, and placeholder text unless they contain actual written content.\n")
	builder.WriteString("Answer using only the information in the notes when possible, and call out uncertainty clearly.\n\n")
	builder.WriteString("Question:\n")
	builder.WriteString(question)
	builder.WriteString("\n\n")
	builder.WriteString("Files to read:\n")

	if len(files) == 0 {
		builder.WriteString("No notes were found for the requested time window.\n")
		return builder.String(), files, nil
	}

	for _, path := range files {
		builder.WriteString("- ")
		builder.WriteString(displayNotePath(config, path))
		builder.WriteString("\n")
	}

	return builder.String(), files, nil
}

func findRecentNotes(config Config, days int) ([]string, error) {
	root := filepath.Join(config.VaultPath, config.NotesDir)
	var matches []string
	cutoff := time.Now().AddDate(0, 0, -(days - 1))

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}

		datePart := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		noteDate, parseErr := time.Parse(dateLayout, datePart)
		if parseErr != nil {
			return nil
		}
		if noteDate.Before(startOfDay(cutoff)) {
			return nil
		}
		if !noteHasSubstantiveContent(path) {
			return nil
		}

		matches = append(matches, path)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	sort.Strings(matches)
	return matches, nil
}

func runCopilotCommand(config Config, prompt string, files []string) error {
	commandTemplate := config.CopilotCommand
	argv, err := splitCommandLine(commandTemplate)
	if err != nil {
		return err
	}
	if len(argv) == 0 {
		return errors.New("copilot command is empty")
	}

	promptFilePath := ""
	if commandUsesPromptFile(argv) {
		promptFilePath, err = writePromptFile(prompt)
		if err != nil {
			return err
		}
		defer os.Remove(promptFilePath)
	}

	for index, arg := range argv {
		argv[index] = strings.ReplaceAll(arg, "{prompt}", prompt)
		argv[index] = strings.ReplaceAll(argv[index], "{vault_path}", config.VaultPath)
		if promptFilePath != "" {
			argv[index] = strings.ReplaceAll(argv[index], "{prompt_file}", promptFilePath)
		}
	}

	argv = maybeAugmentCopilotArgs(argv, config)

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "RETRO_PROMPT="+prompt)
	cmd.Env = append(cmd.Env, "RETRO_VAULT_PATH="+config.VaultPath)
	cmd.Env = append(cmd.Env, "RETRO_NOTES_DIR="+config.NotesDir)
	cmd.Env = append(cmd.Env, "RETRO_NOTE_FILES="+strings.Join(files, string(os.PathListSeparator)))
	if promptFilePath != "" {
		cmd.Env = append(cmd.Env, "RETRO_PROMPT_FILE="+promptFilePath)
	}
	return cmd.Run()
}

func maybeAugmentCopilotArgs(argv []string, config Config) []string {
	if len(argv) == 0 {
		return argv
	}

	commandName := strings.ToLower(filepath.Base(argv[0]))
	if commandName != "copilot" && commandName != "copilot.exe" && commandName != "copilot.ps1" && commandName != "copilot.bat" {
		return argv
	}

	hasAllowAllPaths := false
	hasAddDir := false
	hasChangeDir := false
	for _, arg := range argv[1:] {
		if arg == "--allow-all-paths" {
			hasAllowAllPaths = true
		}
		if arg == "--add-dir" || strings.HasPrefix(arg, "--add-dir=") {
			hasAddDir = true
		}
		if arg == "-C" || strings.HasPrefix(arg, "-C=") {
			hasChangeDir = true
		}
	}

	if !hasChangeDir {
		argv = append([]string{argv[0], "-C", config.VaultPath}, argv[1:]...)
	}
	if !hasAllowAllPaths {
		argv = append(argv, "--allow-all-paths")
	}
	if !hasAddDir {
		argv = append(argv, "--add-dir", config.VaultPath)
	}
	return argv
}

func commandUsesPromptFile(argv []string) bool {
	for _, arg := range argv {
		if strings.Contains(arg, "{prompt_file}") {
			return true
		}
	}
	return false
}

func displayNotePath(config Config, path string) string {
	relativePath, err := filepath.Rel(config.VaultPath, path)
	if err != nil {
		return path
	}
	if strings.HasPrefix(relativePath, "..") {
		return path
	}
	return relativePath
}

func noteHasSubstantiveContent(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return true
	}

	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := normalizeTemplateLine(scanner.Text())
		if line == "" {
			continue
		}
		if isTemplateOnlyLine(line) {
			continue
		}
		return true
	}
	if scanner.Err() != nil {
		return true
	}
	return false
}

func normalizeTemplateLine(line string) string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "- ")
	trimmed = strings.TrimPrefix(trimmed, "#")
	trimmed = strings.TrimSpace(trimmed)
	trimmed = strings.Trim(trimmed, "*")
	trimmed = strings.Trim(trimmed, "`")
	return strings.TrimSpace(trimmed)
}

func isTemplateOnlyLine(line string) bool {
	lower := strings.ToLower(line)
	if lower == "daily log" || lower == "today's standup note" {
		return true
	}
	if strings.HasPrefix(lower, "date:") {
		return true
	}
	if strings.HasPrefix(lower, "from \"worklog/quarterly goals/the goals\"") {
		return true
	}
	if strings.HasPrefix(lower, "flatten file.lists as") || strings.HasPrefix(lower, "list l.text") {
		return true
	}
	if strings.HasSuffix(lower, ":") {
		switch lower {
		case "work focus:", "primary task:", "secondary tasks:", "customer service (internal support):", "requests handled:", "feedback/follow-up:", "progress towards goals:", "goal progress:", "challenges/blockers:", "reflections & notes:", "learnings:", "thoughts/feelings:", "tomorrow's focus:", "top priority:", "goals list reminder:":
			return true
		}
	}
	if strings.HasSuffix(lower, "?") {
		switch lower {
		case "what was the main thing you worked on today?", "what else did you tackle?", "what internal support requests did you handle today?", "did you get any feedback or need to follow up on previous issues?", "what progress did you make today toward your quarterly goals?", "what obstacles did you face, if any?", "what new insights did you gain today?", "how did you feel today (motivation, energy, stress)?", "what is the top priority for tomorrow?":
			return true
		}
	}
	return false
}

func writePromptFile(prompt string) (string, error) {
	file, err := os.CreateTemp("", "retro-prompt-*.md")
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := file.WriteString(prompt); err != nil {
		return "", err
	}
	return file.Name(), nil
}

func splitCommandLine(command string) ([]string, error) {
	runes := []rune(command)
	var args []string
	var current strings.Builder
	inQuote := false
	var quoteChar rune

	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}

	for index := 0; index < len(runes); index++ {
		r := runes[index]
		next := rune(0)
		if index+1 < len(runes) {
			next = runes[index+1]
		}

		switch {
		case inQuote:
			if r == '\\' && next == quoteChar {
				current.WriteRune(next)
				index++
				continue
			}
			if r == quoteChar {
				inQuote = false
			} else {
				current.WriteRune(r)
			}
		case r == '\\' && (next == '\'' || next == '"'):
			current.WriteRune(next)
			index++
			continue
		case r == '\'' || r == '"':
			inQuote = true
			quoteChar = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			current.WriteRune(r)
		}
	}

	if inQuote {
		return nil, fmt.Errorf("unterminated quote in command %s", strconv.Quote(command))
	}
	flush()
	return args, nil
}

func normalizeSection(name string) (sectionSpec, error) {
	lookup := strings.ToLower(strings.TrimSpace(name))
	for _, spec := range sectionSpecs {
		if lookup == spec.Section || lookup == spec.Alias {
			return spec, nil
		}
	}
	return sectionSpec{}, fmt.Errorf("unsupported entry type %q", name)
}

func ensureDailyNote(config Config, noteDate time.Time) (string, error) {
	notePath := dailyNotePath(config, noteDate)
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(notePath); err == nil {
		return notePath, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	content := dailyNoteTemplate(noteDate)
	if err := os.WriteFile(notePath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return notePath, nil
}

func dailyNotePath(config Config, noteDate time.Time) string {
	year := noteDate.Format("2006")
	month := noteDate.Format("01")
	fileName := noteDate.Format(dateLayout) + ".md"
	return filepath.Join(config.VaultPath, config.NotesDir, year, month, fileName)
}

func dailyNoteTemplate(noteDate time.Time) string {
	var builder strings.Builder
	builder.WriteString("# ")
	builder.WriteString(noteDate.Format(dateLayout))
	builder.WriteString("\n\n")
	for _, key := range sectionOrder {
		builder.WriteString("## ")
		builder.WriteString(sectionSpecs[key].Heading)
		builder.WriteString("\n\n")
	}
	return builder.String()
}

func appendBulletToSection(notePath string, heading string, text string) error {
	raw, err := os.ReadFile(notePath)
	if err != nil {
		return err
	}

	content := string(raw)
	marker := "## " + heading
	markerIndex := strings.Index(content, marker)
	if markerIndex == -1 {
		return fmt.Errorf("section %q not found in %s", heading, notePath)
	}

	sectionStart := markerIndex + len(marker)
	sectionEnd := len(content)
	if nextSection := strings.Index(content[sectionStart:], "\n## "); nextSection != -1 {
		sectionEnd = sectionStart + nextSection
	}

	body := strings.Trim(content[sectionStart:sectionEnd], "\r\n")
	var sectionBuilder strings.Builder
	sectionBuilder.WriteString(marker)
	sectionBuilder.WriteString("\n\n")
	if body != "" {
		sectionBuilder.WriteString(body)
		sectionBuilder.WriteString("\n")
	}
	sectionBuilder.WriteString("- ")
	sectionBuilder.WriteString(strings.TrimSpace(text))
	sectionBuilder.WriteString("\n\n")

	remainder := strings.TrimPrefix(content[sectionEnd:], "\n")
	updated := content[:markerIndex] + sectionBuilder.String() + remainder
	return os.WriteFile(notePath, []byte(updated), 0o644)
}

func loadConfig() (Config, error) {
	configPath, err := configPath()
	if err != nil {
		return Config{}, err
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("retro is not configured yet; run 'retro init --vault <path>' first")
		}
		return Config{}, err
	}

	var config Config
	if err := json.Unmarshal(content, &config); err != nil {
		return Config{}, err
	}
	if strings.TrimSpace(config.VaultPath) == "" {
		return Config{}, errors.New("retro config is missing vault_path")
	}
	if strings.TrimSpace(config.NotesDir) == "" {
		config.NotesDir = "Worklog"
	}
	if strings.TrimSpace(config.CopilotCommand) == "" {
		config.CopilotCommand = `copilot -C {vault_path} -p {prompt} --allow-all-tools -s`
	}
	return config, nil
}

func saveConfig(config Config) error {
	configPath, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(configPath, encoded, 0o644)
}

func configPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv("RETRO_CONFIG")); override != "" {
		return filepath.Abs(override)
	}

	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configRoot, "retro", "config.json"), nil
}

func mustConfigPath() string {
	path, err := configPath()
	if err != nil {
		return "<unknown>"
	}
	return path
}

func startOfDay(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}
