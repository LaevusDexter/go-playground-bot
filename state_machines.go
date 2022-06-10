package main

// state machine
type machine = [][][]byte

// state
type state = [][]byte

// jump
type jmp = []byte

type job = []byte

const (
	caseDefault = iota

	// argument parser
	caseSeparator
	caseOption
	caseAssignment
)

/*
	jobs:
		[1] set command
		[2] set option name
		[3] set option existence
		[4] set option value
		[5] done
*/

var argumentSM = machine{
	/*
		command
	*/
	state{jmp{0, 1, 2, 0}, job{0, 1, 1, 0}, job{0, 0, 0, 0}, job{0, 0, 0, 0}, job{0, 0, 0, 0}, job{0, 0, 0, 0}}, // 0
	/*
		searching for the option
	*/
	state{jmp{1, 1, 2, 1}, job{0, 0, 0, 0}, job{0, 0, 0, 0}, job{0, 0, 0, 0}, job{0, 0, 0, 0}, job{1, 0, 0, 1}}, // 1

	/*
		option
	*/
	state{jmp{2, 1, 2, 3}, job{0, 0, 0, 0}, job{0, 1, 0, 1}, job{0, 1, 0, 0}, job{0, 0, 0, 0}, job{0, 0, 1, 0}}, // 2

	/*
		reading the argument
	*/
	state{jmp{3, 1, 2, 1}, job{0, 0, 0, 0}, job{0, 0, 0, 0}, job{0, 0, 0, 0}, job{0, 1, 1, 1}, job{0, 0, 0, 1}}, // 3
}

func check(b byte) bool {
	if b > 0 {
		return true
	}

	return false
}
