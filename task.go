package task

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"
	"os/exec"

	"github.com/BurntSushi/toml"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
)

var (
	// TaskFilePath is the default Taskfile
	TaskFilePath = "Taskfile"
	// ShExists is true if Bash was found
	ShExists bool
	// ShPath constains the Bash path if found
	ShPath string

	// Force (--force or -f flag) forces a task to run even when it's up-to-date
	Force bool

	// Tasks constains the tasks parsed from Taskfile
	Tasks = make(map[string]*Task)

	runnedTasks = make(map[string]struct{})
)

func init() {
	var err error
	ShPath, err = exec.LookPath("sh")
	if err != nil {
		return
	}
	ShExists = true
}

// Task represents a task
type Task struct {
	Cmds      []string
	Deps      []string
	Sources   []string
	Generates []string
	Dir       string
	Vars      map[string]string
	Set       string
}

// Run runs Task
func Run() {
	log.SetFlags(0)

	args := pflag.Args()
	if len(args) == 0 {
		log.Fatal("task: No argument given")
	}

	var err error
	Tasks, err = readTaskfile()
	if err != nil {
		log.Fatal(err)
	}

	for _, a := range args {
		if err = RunTask(a); err != nil {
			log.Fatal(err)
		}
	}
}

// RunTask runs a task by its name
func RunTask(name string) error {
	if _, found := runnedTasks[name]; found {
		return &cyclicDepError{name}
	}
	runnedTasks[name] = struct{}{}

	t, ok := Tasks[name]
	if !ok {
		return &taskNotFoundError{name}
	}

	vars, err := t.handleVariables()
	if err != nil {
		return &taskRunError{name, err}
	}

	for _, d := range t.Deps {
		if err := RunTask(ReplaceVariables(d, vars)); err != nil {
			return err
		}
	}

	if !Force && t.isUpToDate() {
		log.Printf(`task: Task "%s" is up to date`, name)
		return nil
	}

	for i := range t.Cmds {
		if err = t.runCommand(i); err != nil {
			return &taskRunError{name, err}
		}
	}
	return nil
}

func (t *Task) isUpToDate() bool {
	if len(t.Sources) == 0 || len(t.Generates) == 0 {
		return false
	}

	sourcesMaxTime, err := getPatternsMaxTime(t.Sources)
	if err != nil || sourcesMaxTime.IsZero() {
		return false
	}

	generatesMinTime, err := getPatternsMinTime(t.Generates)
	if err != nil || generatesMinTime.IsZero() {
		return false
	}

	return generatesMinTime.After(sourcesMaxTime)
}

func (t *Task) runCommand(i int) error {
	vars, err := t.handleVariables()
	if err != nil {
		return err
	}
	var (
		c   = ReplaceVariables(t.Cmds[i], vars)
		dir = ReplaceVariables(t.Dir, vars)
		cmd *exec.Cmd
	)
	if ShExists {
		cmd = exec.Command(ShPath, "-c", c)
	} else {
		cmd = exec.Command("cmd", "/C", c)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	if t.Set != "" {
		bytes, err := cmd.Output()
		if err != nil {
			return err
		}
		os.Setenv(t.Set, string(bytes))
		return nil
	}
	cmd.Stdout = os.Stdout
	if err = cmd.Run(); err != nil {
		return err
	}
	return nil
}

func readTaskfile() (tasks map[string]*Task, err error) {
	if b, err := ioutil.ReadFile(TaskFilePath + ".yml"); err == nil {
		return tasks, yaml.Unmarshal(b, &tasks)
	}
	if b, err := ioutil.ReadFile(TaskFilePath + ".json"); err == nil {
		return tasks, json.Unmarshal(b, &tasks)
	}
	if b, err := ioutil.ReadFile(TaskFilePath + ".toml"); err == nil {
		return tasks, toml.Unmarshal(b, &tasks)
	}
	return nil, ErrNoTaskFile
}
