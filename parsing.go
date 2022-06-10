package main

import (
	"bytes"
	"strings"
)

func catchPrefix(content string, prefix string, botID string) string {
	buf := make([]byte, 0, 16)
	within := false

	for i := 0; i < len(content); i++ {
		if within && content[i] == '>' && b2s(buf) == botID {
			return strings.TrimSpace(content[i+1:])
		}

		buf = append(buf, content[i])

		switch b2s(buf) {
		case prefix:
			return content[i+1:]
		case "<@":
			within = true
			buf = buf[:0]
		case "<":
		case " ", "\n", "\r", "\t":
			return ""
		case "!":
			if within {
				buf = buf[:0]
			}
		}
	}

	return ""
}

type parsingResult struct {
	content string
	command string
	options map[string]interface{}
}

func parseCommand(content string, separators string, optionMarkers []string, assignmentMarker []string) *parsingResult {
	var (
		start, end int
		startValue int
		cs         int

		option  string
		command string

		currentState byte
	)

	options := make(map[string]interface{})

	var i int
	for i = 0; i < len(content); i++ {
		size := 0

		switch {
		case hasPrefix(content, i, optionMarkers, &size):
			cs = caseOption

			start = i + size
		case hasPrefix(content, i, assignmentMarker, &size):
			cs = caseAssignment

			startValue = i + size
			end = i
		case bytes.IndexByte(s2b(separators), content[i]) != -1:
			cs = caseSeparator
			end = i
		default:
			cs = caseDefault
		}

		state := argumentSM[currentState]

		if check(state[5][cs]) {
			i--

			break
		}

		if check(state[1][cs]) {
			command = content[:i]
		}

		if check(state[2][cs]) {
			option = content[start:end]
		}

		if check(state[3][cs]) {
			options[option] = true
		}

		if check(state[4][cs]) {
			options[option] = content[startValue:i]
		}

		currentState = argumentSM[currentState][0][cs]
	}

	switch {
	case i != len(content):
		break
	case currentState == 0:
		command = content
	case currentState == 2:
		options[content[start:]] = true
	case currentState == 3:
		options[option] = content[startValue:]
	}

	return &parsingResult{
		content: content[i:],
		options: options,
		command: command,
	}
}

func hasPrefix(content string, pos int, variations []string, size *int) bool {
	for _, v := range variations {
		if strings.HasPrefix(content[pos:], v) {
			*size = len(v)

			return true
		}
	}

	return false
}
