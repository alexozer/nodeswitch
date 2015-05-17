package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"syscall"
)

const numFifos = 5
const doneCmd = "done"

var (
	fifoDir   = os.TempDir() + "/nodeswitch"
	stateJson = fifoDir + "/state.json"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Must specify app file or command.")
		return
	}

	var err error

	var state *State
	if exists, err := exists(fifoDir); !exists {
		if err != nil {
			log.Fatal(err)
		}

		if os.Args[1] == doneCmd {
			return
		}

		if err = os.Mkdir(fifoDir, 0755); err != nil {
			log.Fatal(err)
		}

		state, err = initNodes()
		if err != nil {
			log.Fatal(err)
		}
	} else {
		state, err = readState(stateJson)
		if err != nil {
			log.Fatal(err)
		}

		if state.Running {
			log.Fatal("nodeswitch is already running.")
		}
	}

	if os.Args[1] == doneCmd {
		err = cleanup(state)
		if err != nil {
			log.Fatal(err)
		}

		return
	}

	state.Running = true
	if err = state.Write(stateJson); err != nil {
		log.Fatal(err)
	}

	// check if the last node process is still running, and if so, kill it
	lastProcess, err := os.FindProcess(state.Nodes[state.CurrNode])
	if err == nil {
		lastProcess.Signal(os.Interrupt)
	}

	// start a new process in its place
	state.StartProcess(state.CurrNode)

	state.CurrNode = (state.CurrNode + 1) % len(state.Nodes)
	if err = state.StartApp(os.Args[1]); err != nil {
		log.Fatal(err)
	}

	state.Running = false
	if err := state.Write(stateJson); err != nil {
		log.Fatal(err)
	}
}

func initNodes() (*State, error) {
	// start every node process except the last one
	state := State{CurrNode: numFifos - 1}
	state.Nodes = make([]int, numFifos)
	for i := range state.Nodes {
		if err := MakeFifo(i); err != nil {
			return nil, err
		}

		if i == len(state.Nodes)-1 {
			continue
		}

		if err := state.StartProcess(i); err != nil {
			return nil, err
		}
	}

	return &state, nil
}

func (this *State) StartProcess(fid int) error {
	cmd := exec.Command("node", fifoPath(fid))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	this.Nodes[fid] = cmd.Process.Pid

	return nil
}

func (this *State) EndProcess(fid int) error {
	process, err := os.FindProcess(this.Nodes[fid])
	if err != nil {
		return err
	}
	process.Signal(os.Interrupt)

	return nil
}

func MakeFifo(fid int) error {
	path := fifoPath(fid)
	err := os.Remove(path)
	if !os.IsNotExist(err) && err != nil {
		return err
	}

	return syscall.Mkfifo(path, uint32(os.ModeNamedPipe|0644))
}

func fifoPath(fid int) string {
	return fmt.Sprintf("%s/fifo%d", fifoDir, fid)
}

func (this *State) StartApp(path string) error {
	// reads in the entire app js file, so it's assumed it isn't too large
	appBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(fifoPath(this.CurrNode), appBytes, os.ModePerm)
	if err != nil {
		return err
	}

	return nil
}

type State struct {
	Running  bool
	CurrNode int
	Nodes    []int
}

func readState(path string) (*State, error) {
	jsonStr, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var state State
	err = json.Unmarshal(jsonStr, &state)
	if err != nil {
		return nil, err
	}

	return &state, nil
}

func (this *State) Write(path string) error {
	err := os.Remove(path)
	if !os.IsNotExist(err) && err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	b, err := json.Marshal(this)
	if err != nil {
		return err
	}

	_, err = file.Write(b)
	return err
}

func cleanup(state *State) error {
	for _, pid := range state.Nodes {
		proc, err := os.FindProcess(pid)
		if err == nil {
			proc.Signal(os.Interrupt)
		}
	}

	return os.RemoveAll(fifoDir)
}

func exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
