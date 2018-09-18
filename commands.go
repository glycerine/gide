// Copyright (c) 2018, The Gide Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gide

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/goki/gi"
	"github.com/goki/gi/giv"
	"github.com/goki/gi/oswin"
	"github.com/goki/ki"
	"github.com/goki/ki/kit"
)

// CmdAndArgs contains the name of an external program to execute and args to
// pass to that program
type CmdAndArgs struct {
	Cmd  string   `desc:"external program to execute -- must be on path or have full path specified -- use {RunExec} for the project RunExec executable."`
	Args []string `desc:"args to pass to the program, one string per arg -- use {FileName} etc to refer to special variables -- just start typing { and you'll get a completion menu of options, and use \{ to insert a literal curly bracket.  A '/' path separator directly between path variables will be replaced with \ on Windows."`
}

// HasPrompts returns true if any prompts are required before running command,
// and the set of such args
func (cm *CmdAndArgs) HasPrompts() (map[string]struct{}, bool) {
	var ps map[string]struct{}
	for _, av := range cm.Args {
		aps, has := ArgVarPrompts(av)
		if has {
			if ps == nil {
				ps = aps
			} else {
				for key, _ := range aps {
					ps[key] = struct{}{}
				}
			}
		}
	}
	if len(ps) > 0 {
		return ps, true
	} else {
		return nil, false
	}
}

// BindArgs replaces any variables in the args with their values, and returns resulting args
func (cm *CmdAndArgs) BindArgs() []string {
	sz := len(cm.Args)
	if sz == 0 {
		return nil
	}
	args := make([]string, sz)
	for i := range cm.Args {
		av := BindArgVars(cm.Args[i])
		args[i] = av
	}
	return args
}

// PrepCmd prepares to run command, returning *exec.Cmd and a string of the full command
func (cm *CmdAndArgs) PrepCmd() (*exec.Cmd, string) {
	cstr := BindArgVars(cm.Cmd)
	cmdstr := cstr
	args := cm.BindArgs()
	if args != nil {
		astr := strings.Join(args, " ")
		cmdstr += " " + astr
	}
	cmd := exec.Command(cstr, args...)
	return cmd, cmdstr
}

// Command defines different types of commands that can be run in the project.
// The output of the commands shows up in an associated tab.
type Command struct {
	Name  string       `desc:"name of this type of project (must be unique in list of such types)"`
	Desc  string       `desc:"brief description of this command"`
	Langs LangNames    `desc:"language(s) that this command applies to -- leave empty if it applies to any -- filters the list of commands shown based on file language type"`
	Cmds  []CmdAndArgs `tableview-select:"-" desc:"sequence of commands to run for this overall command."`
	Dir   string       `desc:"if specified, will change to this directory before executing the command -- e.g., use {FileDirPath} for current file's directory -- only use directory values here -- if not specified, directory will be project root directory."`
	Wait  bool         `desc:"if true, we wait for the command to run before displaying output -- for quick commands and those where subsequent steps. If multiple commands are present, then subsequent steps always wait for prior steps in the sequence"`
	Buf   *giv.TextBuf `tableview:"-" view:"-" desc:"text buffer for displaying output of command"`
}

// MakeBuf creates the buffer object to save output from the command -- if
// this is not called in advance of Run, then output is ignored.  returns true
// if a new buffer was created, false if one already existed -- if clear is
// true, then any existing buffer is cleared.
func (cm *Command) MakeBuf(clear bool) bool {
	if cm.Buf != nil {
		if clear {
			cm.Buf.New(0)
		}
		return false
	}
	cm.Buf = &giv.TextBuf{}
	cm.Buf.InitName(cm.Buf, cm.Name+"-buf")
	return true
}

// HasPrompts returns true if any prompts are required before running command,
// and the set of such args
func (cm *Command) HasPrompts() (map[string]struct{}, bool) {
	var ps map[string]struct{}
	for i := range cm.Cmds {
		cma := &cm.Cmds[i]
		aps, has := cma.HasPrompts()
		if has {
			if ps == nil {
				ps = aps
			} else {
				for key, _ := range aps {
					ps[key] = struct{}{}
				}
			}
		}
	}
	if len(ps) > 0 {
		return ps, true
	} else {
		return nil, false
	}
}

// CmdNoUserPrompt can be set to true to prevent user from being prompted for strings
// this is useful when a custom outer-loop has already set the string values.
// this will be reset automatically after command is run.
var CmdNoUserPrompt bool

// PromptUser prompts for values that need prompting for, and then runs
// RunAfterPrompts if not otherwise cancelled by user
func (cm *Command) PromptUser(ge *Gide, pvals map[string]struct{}) {
	sz := len(pvals)
	cnt := 0
	for pv, _ := range pvals {
		switch pv {
		case "{PromptString1}":
			fallthrough
		case "{PromptString2}":
			gi.StringPromptDialog(ge.Viewport, "", "Enter string value here..",
				gi.DlgOpts{Title: pv, Prompt: "Enter string value for executing command: " + cm.Name},
				ge.This, func(recv, send ki.Ki, sig int64, data interface{}) {
					dlg := send.(*gi.Dialog)
					if sig == int64(gi.DialogAccepted) {
						val := gi.StringPromptDialogValue(dlg)
						ArgVarVals[pv] = val
						cnt++
						if cnt == sz {
							cm.RunAfterPrompts(ge)
						}
					}
				})
		}
	}
}

// Run runs the command and saves the output in the Buf if it is non-nil,
// which can be displayed -- if !wait, then Buf is updated online as output
// occurs.  Status is updated with status of command exec.  User is prompted
// for any values that might be needed for command.
func (cm *Command) Run(ge *Gide) {
	pvals, hasp := cm.HasPrompts()
	if !hasp || CmdNoUserPrompt {
		cm.RunAfterPrompts(ge)
	}
	cm.PromptUser(ge, pvals)
}

// RunAfterPrompts runs after any prompts have been set, if needed
func (cm *Command) RunAfterPrompts(ge *Gide) {
	CmdNoUserPrompt = false
	if cm.Dir != "" {
		cds := BindArgVars(cm.Dir)
		err := os.Chdir(cds)
		cm.AppendCmdOut(ge, []byte(fmt.Sprintf("cd %v (from: %v)", cds, cm.Dir)))
		if err != nil {
			cm.AppendCmdOut(ge, []byte(fmt.Sprintf("Could not change to directory %v -- error: %v", cds, err)))
		}
	}

	if cm.Wait || len(cm.Cmds) > 1 {
		for i := range cm.Cmds {
			cma := &cm.Cmds[i]
			if cm.Buf == nil {
				if !cm.RunNoBuf(ge, cma) {
					break
				}
			} else {
				if !cm.RunBufWait(ge, cma) {
					break
				}
			}
		}
	} else {
		cma := &cm.Cmds[0]
		if cm.Buf == nil {
			go cm.RunNoBuf(ge, cma)
		} else {
			go cm.RunBuf(ge, cma)
		}
	}

	cds := BindArgVars("{ProjPath}")
	err := os.Chdir(cds)
	if err != nil { // shouldn't happen
		log.Printf("Could not change to proj directory %v (spec: {ProjPath}): error: %v", cds, err)
	}
}

// RunBufWait runs a command with output to the buffer, using CombinedOutput
// so it waits for completion -- returns overall command success, and logs one
// line of the command output to gide statusbar
func (cm *Command) RunBufWait(ge *Gide, cma *CmdAndArgs) bool {
	cmd, cmdstr := cma.PrepCmd()
	out, err := cmd.CombinedOutput()
	cm.AppendCmdOut(ge, out)
	return cm.RunStatus(ge, cmdstr, err, out)
}

// RunBuf runs a command with output to the buffer, incrementally updating the
// buffer with new results line-by-line as they come in
func (cm *Command) RunBuf(ge *Gide, cma *CmdAndArgs) bool {
	cmd, cmdstr := cma.PrepCmd()
	stdout, err := cmd.StdoutPipe()
	if err == nil {
		cmd.Stderr = cmd.Stdout
		err = cmd.Start()
		if err == nil {
			outscan := bufio.NewScanner(stdout) // line at a time
			for outscan.Scan() {
				cm.Buf.AppendTextLine(MarkupCmdOutput(outscan.Bytes()))
			}
		}
		err = cmd.Wait()
	}
	return cm.RunStatus(ge, cmdstr, err, nil)
}

// RunNoBuf runs a command without any output to the buffer -- can call using
// go as a goroutine for no-wait case -- returns overall command success, and
// logs one line of the command output to gide statusbar
func (cm *Command) RunNoBuf(ge *Gide, cma *CmdAndArgs) bool {
	cmd, cmdstr := cma.PrepCmd()
	out, err := cmd.CombinedOutput()
	return cm.RunStatus(ge, cmdstr, err, out)
}

// AppendCmdOut appends command output to buffer, applying markup for links
func (cm *Command) AppendCmdOut(ge *Gide, out []byte) {
	if cm.Buf == nil {
		return
	}
	// todo: add update start / end to textbuf
	lns := bytes.Split(out, []byte("\n"))
	for _, txt := range lns {
		cm.Buf.AppendTextLine(MarkupCmdOutput(txt))
	}
}

// CmdOutStatusLen is amount of command output to include in the status update
var CmdOutStatusLen = 80

// RunStatus reports the status of the command run (given in cmdstr) to
// ge.StatusBar -- returns true if there are no errors, and false if there
// were errors
func (cm *Command) RunStatus(ge *Gide, cmdstr string, err error, out []byte) bool {
	rval := true
	outstr := ""
	if out != nil {
		outstr = string(out[:CmdOutStatusLen])
	}
	finstat := ""
	tstr := time.Now().Format("Mon Jan  2 15:04:05 MST 2006")
	if err == nil {
		finstat = fmt.Sprintf("%v <b>successful</b> at: %v", cmdstr, tstr)
		rval = true
	} else if ee, ok := err.(*exec.ExitError); ok {
		finstat = fmt.Sprintf("%v <b>failed</b> at: %v with error: %v", cmdstr, tstr, ee.Error())
		rval = false
	} else {
		finstat = fmt.Sprintf("%v <b>exec error</b> at: %v error: %v", cmdstr, tstr, err.Error())
		rval = false
	}
	cm.Buf.AppendTextLine([]byte("\n"))
	cm.Buf.AppendTextLine(MarkupCmdOutput([]byte(finstat)))
	cm.Buf.Refresh()
	ge.SetStatus(cmdstr + " " + outstr)
	return rval
}

// LangMatch returns true if the given languages match those of the command,
// or command has no language restrictions
func (cm *Command) LangMatch(langs LangNames) bool {
	if len(cm.Langs) == 0 {
		return true
	}
	if len(langs) == 0 {
		return false
	}
	for _, cln := range cm.Langs {
		for _, lnm := range langs {
			if cln == lnm {
				return true
			}
		}
	}
	return false
}

// MarkupCmdOutput applies links to the first element in command output line
// if it looks like a file name / position
func MarkupCmdOutput(out []byte) []byte {
	flds := bytes.Fields(out)
	if len(flds) == 0 {
		return out
	}
	mx := gi.MinInt(len(flds), 2)
	for i := 0; i < mx; i++ {
		ff := flds[i]
		if !(bytes.Contains(ff, []byte(".")) || bytes.Contains(ff, []byte("/"))) { // extension or path
			continue
		}
		fnflds := bytes.Split(ff, []byte(":"))
		fn := string(fnflds[0])
		pos := ""
		col := ""
		if len(fnflds) > 1 {
			pos = string(fnflds[1])
			col = ""
			if len(fnflds) > 2 {
				col = string(fnflds[2])
			}
		}
		cpath := ArgVarVals["{FileDirPath}"]
		if !strings.HasPrefix(fn, cpath) {
			fn = filepath.Join(cpath, strings.TrimPrefix(fn, "./"))
		}
		link := ""
		if col != "" {
			link = fmt.Sprintf(`<a href="file:///%v#L%vC%v">%v</a>`, fn, pos, col, string(ff))
		} else if pos != "" {
			link = fmt.Sprintf(`<a href="file:///%v#L%v">%v</a>`, fn, pos, string(ff))
		} else {
			link = fmt.Sprintf(`<a href="file:///%v">%v</a>`, fn, string(ff))
		}
		flds[i] = []byte(link)
		break
	}
	jf := bytes.Join(flds, []byte(" "))
	return jf
}

////////////////////////////////////////////////////////////////////////////////
//  Commands

// Commands is a list of different commands
type Commands []*Command

var KiT_Commands = kit.Types.AddType(&Commands{}, CommandsProps)

// CmdName has an associated ValueView for selecting from the list of
// available command names, for use in preferences etc.
type CmdName string

// IsValid checks if command name exists on AvailCmds list
func (cn CmdName) IsValid() bool {
	_, _, ok := AvailCmds.CmdByName(cn)
	return ok
}

// Command returns command associated with command name in AvailCmds, and
// false if it doesn't exist
func (cn CmdName) Command() (*Command, bool) {
	cmd, _, ok := AvailCmds.CmdByName(cn)
	return cmd, ok
}

// CmdNames is a slice of command names
type CmdNames []CmdName

// Add adds a name to the list
func (cn *CmdNames) Add(cmd CmdName) {
	*cn = append(*cn, cmd)
}

// AvailCmds is the current list of ALL available commands for use -- it
// combines StdCmds and CustomCmds.  Custom overrides Std items with
// the same names.
var AvailCmds Commands

// CustomCmds is user-specific list of commands saved in preferences available
// for all Gide projects.  These will override StdCmds with the same names.
var CustomCmds Commands

// LangCmdNames returns a slice of commands that are compatible with given
// language(s).
func (cm *Commands) LangCmdNames(langs LangNames) []string {
	cmds := make([]string, 0, 100)
	for _, cmd := range *cm {
		if cmd.LangMatch(langs) {
			cmds = append(cmds, cmd.Name)
		}
	}
	return cmds
}

// VersCtrlCmdNames returns a slice of commands that contain in their name the
// specific version control name, but NOT the others -- takes the output of LangCmdNames
func VersCtrlCmdNames(vcnm VersCtrlName, cmds []string) []string {
	if vcnm == "" {
		return cmds
	}
	sz := len(cmds)
	for i := sz - 1; i >= 0; i-- {
		cmd := cmds[i]
		if strings.Contains(cmd, string(vcnm)) {
			continue
		}
		for _, vcs := range VersCtrlSystems {
			if vcs != string(vcnm) {
				if strings.Contains(cmd, vcs) {
					cmds = append(cmds[:i], cmds[i+1:]...)
				}
			}
		}
	}
	return cmds
}

// FilterCmdNames returns a slice of commands that are compatible with given
// language(s) and version control system.
func (cm *Commands) FilterCmdNames(langs LangNames, vcnm VersCtrlName) []string {
	return VersCtrlCmdNames(vcnm, cm.LangCmdNames(langs))
}

func init() {
	AvailCmds.CopyFrom(StdCmds)
}

// CmdByName returns a command and index by name -- returns false and emits a
// message to stdout if not found
func (cm *Commands) CmdByName(name CmdName) (*Command, int, bool) {
	for i, cmd := range *cm {
		if cmd.Name == string(name) {
			return cmd, i, true
		}
	}
	fmt.Printf("gi.Commands.CmdByName: command named: %v not found\n", name)
	return nil, -1, false
}

// PrefsCmdsFileName is the name of the preferences file in App prefs
// directory for saving / loading your CustomCmds commands list
var PrefsCmdsFileName = "command_prefs.json"

// OpenJSON opens commands from a JSON-formatted file.
func (cm *Commands) OpenJSON(filename gi.FileName) error {
	*cm = make(Commands, 0, 10) // reset
	b, err := ioutil.ReadFile(string(filename))
	if err != nil {
		// gi.PromptDialog(nil, gi.DlgOpts{Title: "File Not Found", Prompt: err.Error()}, true, false, nil, nil)
		// log.Println(err)
		return err
	}
	return json.Unmarshal(b, cm)
}

// SaveJSON saves commands to a JSON-formatted file.
func (cm *Commands) SaveJSON(filename gi.FileName) error {
	b, err := json.MarshalIndent(cm, "", "  ")
	if err != nil {
		log.Println(err) // unlikely
		return err
	}
	err = ioutil.WriteFile(string(filename), b, 0644)
	if err != nil {
		gi.PromptDialog(nil, gi.DlgOpts{Title: "Could not Save to File", Prompt: err.Error()}, true, false, nil, nil)
		log.Println(err)
	}
	return err
}

// OpenPrefs opens custom Commands from App standard prefs directory, using
// PrefsCmdsFileName
func (cm *Commands) OpenPrefs() error {
	pdir := oswin.TheApp.AppPrefsDir()
	pnm := filepath.Join(pdir, PrefsCmdsFileName)
	CustomCmdsChanged = false
	return cm.OpenJSON(gi.FileName(pnm))
}

// SavePrefs saves custom Commands to App standard prefs directory, using
// PrefsCmdsFileName
func (cm *Commands) SavePrefs() error {
	pdir := oswin.TheApp.AppPrefsDir()
	pnm := filepath.Join(pdir, PrefsCmdsFileName)
	CustomCmdsChanged = false
	return cm.SaveJSON(gi.FileName(pnm))
}

// CopyFrom copies commands from given other map
func (cm *Commands) CopyFrom(cp Commands) {
	*cm = make(Commands, 0, len(cp)) // reset
	b, err := json.Marshal(cp)
	if err != nil {
		fmt.Printf("json err: %v\n", err.Error())
	}
	json.Unmarshal(b, cm)
}

// MergeAvailCmds updates the AvailCmds list from CustomCmds and StdCmds
func MergeAvailCmds() {
	AvailCmds.CopyFrom(StdCmds)
	for _, cmd := range CustomCmds {
		_, idx, has := AvailCmds.CmdByName(CmdName(cmd.Name))
		if has {
			AvailCmds[idx] = cmd // replace
		} else {
			AvailCmds = append(AvailCmds, cmd)
		}
	}
}

// ViewStd shows the standard types that are compiled into the program and have
// all the lastest standards.  Useful for comparing against custom lists.
func (cm *Commands) ViewStd() {
	CmdsView(&StdCmds)
}

// CustomCmdsChanged is used to update giv.CmdsView toolbars via following
// menu, toolbar props update methods.
var CustomCmdsChanged = false

// CommandsProps define the ToolBar and MenuBar for TableView of Commands, e.g., CmdsView
var CommandsProps = ki.Props{
	"MainMenu": ki.PropSlice{
		{"AppMenu", ki.BlankProp{}},
		{"File", ki.PropSlice{
			{"OpenPrefs", ki.Props{}},
			{"SavePrefs", ki.Props{
				"shortcut": "Command+S",
				"updtfunc": func(cmi interface{}, act *gi.Action) {
					act.SetActiveState(CustomCmdsChanged)
				},
			}},
			{"sep-file", ki.BlankProp{}},
			{"OpenJSON", ki.Props{
				"label":    "Open from file",
				"desc":     "You can save and open commands to / from files to share, experiment, transfer, etc",
				"shortcut": "Command+O",
				"Args": ki.PropSlice{
					{"File Name", ki.Props{
						"ext": ".json",
					}},
				},
			}},
			{"SaveJSON", ki.Props{
				"label": "Save to file",
				"desc":  "You can save and open commands to / from files to share, experiment, transfer, etc",
				"Args": ki.PropSlice{
					{"File Name", ki.Props{
						"ext": ".json",
					}},
				},
			}},
		}},
		{"Edit", "Copy Cut Paste Dupe"},
		{"Window", "Windows"},
	},
	"ToolBar": ki.PropSlice{
		{"SavePrefs", ki.Props{
			"desc": "saves Commands to App standard prefs directory, in file proj_types_prefs.json, which will be loaded automatically at startup if prefs SaveCommands is checked (should be if you're using custom commands)",
			"icon": "file-save",
			"updtfunc": func(cmi interface{}, act *gi.Action) {
				act.SetActiveStateUpdt(CustomCmdsChanged)
			},
		}},
		{"sep-file", ki.BlankProp{}},
		{"OpenJSON", ki.Props{
			"label": "Open from file",
			"icon":  "file-open",
			"desc":  "You can save and open commands to / from files to share, experiment, transfer, etc",
			"Args": ki.PropSlice{
				{"File Name", ki.Props{
					"ext": ".json",
				}},
			},
		}},
		{"SaveJSON", ki.Props{
			"label": "Save to file",
			"icon":  "file-save",
			"desc":  "You can save and open commands to / from files to share, experiment, transfer, etc",
			"Args": ki.PropSlice{
				{"File Name", ki.Props{
					"ext": ".json",
				}},
			},
		}},
		{"sep-std", ki.BlankProp{}},
		{"ViewStd", ki.Props{
			"desc":    "Shows the standard commands that are compiled into the program.  Custom commands override standard ones of the same name.",
			"confirm": true,
		}},
	},
}

// StdCmds is the original compiled-in set of standard commands.
var StdCmds = Commands{
	{"Run Proj", "run RunExec executable set in project", nil,
		[]CmdAndArgs{CmdAndArgs{"{RunExec}", nil}}, "", false, nil},

	// Go
	{"Imports Go File", "run goimports on file", LangNames{"Go"},
		[]CmdAndArgs{CmdAndArgs{"goimports", []string{"-w", "{FilePath}"}}}, "{FileDirPath}", true, nil},
	{"Fmt Go File", "run go fmt on file", LangNames{"Go"},
		[]CmdAndArgs{CmdAndArgs{"gofmt", []string{"-w", "{FilePath}"}}}, "{FileDirPath}", true, nil},
	{"Build Go File", "run go build to build in current dir", LangNames{"Go"},
		[]CmdAndArgs{CmdAndArgs{"go", []string{"build", "-v", "{FileDirPath}"}}}, "{FileDirPath}", false, nil},
	{"Build Go Proj", "run go build for project BuildDir", LangNames{"Go"},
		[]CmdAndArgs{CmdAndArgs{"go", []string{"build", "-v", "{BuildDir}"}}}, "{BuildDir}", false, nil},
	{"Test Go", "run go test in current dir", LangNames{"Go"},
		[]CmdAndArgs{CmdAndArgs{"go", []string{"test", "-v", "{FileDirPath}"}}}, "{FileDirPath}", false, nil},
	{"Vet Go", "run go vet in current dir", LangNames{"Go"},
		[]CmdAndArgs{CmdAndArgs{"go", []string{"vet", "{FileDirPath}"}}}, "{FileDirPath}", false, nil},

	// Git
	{"Adds Git", "git add file", nil,
		[]CmdAndArgs{CmdAndArgs{"git", []string{"add", "{FilePath}"}}}, "{FileDirPath}", true, nil},
	{"Status Git", "git status", nil,
		[]CmdAndArgs{CmdAndArgs{"git", []string{"status", "{FileDirPath}"}}}, "{FileDirPath}", true, nil},
	{"Log Git", "git log", nil,
		[]CmdAndArgs{CmdAndArgs{"git", []string{"log", "{FileDirPath}"}}}, "{FileDirPath}", false, nil},
	{"Commit Git", "git commit", nil,
		[]CmdAndArgs{CmdAndArgs{"git", []string{"commit", "-am", "{PromptString1}"}}}, "{FileDirPath}", true, nil}, // promptstring1 provided during normal commit process, MUST be wait!
	{"Pull Git ", "git pull", nil,
		[]CmdAndArgs{CmdAndArgs{"git", []string{"pull"}}}, "", true, nil},
	{"Push Git ", "git push", nil,
		[]CmdAndArgs{CmdAndArgs{"git", []string{"push"}}}, "", true, nil},

	// SVN
	{"Adds SVN", "svn add file", nil,
		[]CmdAndArgs{CmdAndArgs{"svn", []string{"add", "{FilePath}"}}}, "{FileDirPath}", true, nil},
	{"Status SVN", "svn status", nil,
		[]CmdAndArgs{CmdAndArgs{"svn", []string{"status", "{FileDirPath}"}}}, "{FileDirPath}", true, nil},
	{"Info SVN", "svn info", nil,
		[]CmdAndArgs{CmdAndArgs{"svn", []string{"info", "{FileDirPath}"}}}, "{FileDirPath}", true, nil},
	{"Log SVN", "svn log", nil,
		[]CmdAndArgs{CmdAndArgs{"svn", []string{"log", "-v", "{FileDirPath}"}}}, "{FileDirPath}", false, nil},
	{"Commit SVN", "svn commit", nil,
		[]CmdAndArgs{CmdAndArgs{"svn", []string{"commit", "-m", "{PromptString1}"}}}, "{FileDirPath}", true, nil}, // promptstring1 provided during normal commit process
	{"Update SVN", "svn update", nil,
		[]CmdAndArgs{CmdAndArgs{"svn", []string{"push"}}}, "", true, nil},

	// LaTeX
	{"LaTeX PDF File", "run PDFLaTeX on file", LangNames{"LaTeX"},
		[]CmdAndArgs{CmdAndArgs{"pdflatex", []string{"-file-line-error", "-interaction=nonstopmode", "{FilePath}"}}}, "{FileDirPath}", false, nil},

	// Misc testing
	{"List Dir", "list current dir -- just for testing", nil,
		[]CmdAndArgs{CmdAndArgs{"ls", []string{"-la"}}}, "{FileDirPath}", false, nil},
	{"Echo prompt", "echo string prompt 1 -- just for testing", nil,
		[]CmdAndArgs{CmdAndArgs{"echo", []string{"{PromptString1}"}}}, "{FileDirPath}", false, nil},
}