package support

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
)

func CheckErr(message string, err error) {
	if err != nil {
		if message != "" {
			fmt.Println(message)
		}
		fmt.Println(err)
		os.Exit(1)
	}
}

//ExecuteSingleCommand this function executes a given command.
func ExecuteSingleCommand(command []string) (string, string, error) {

	cmd := exec.Command(command[0], command[1:]...)
	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()

	err := cmd.Start()

	scannerErr := bufio.NewScanner(stderr)
	errStr := ""
	for scannerErr.Scan() {
		errStr += scannerErr.Text() + "\n"
	}

	scannerOut := bufio.NewScanner(stdout)
	outStr := ""
	for scannerOut.Scan() {
		outStr += scannerOut.Text() + "\n"
	}

	if len(outStr) > 0 {
		outStr = outStr[:len(outStr)-1]
	}
	if len(errStr) > 0 {
		errStr = errStr[:len(errStr)-1]
	}

	err = cmd.Wait()
	return outStr, errStr, err

}

//ExecuteCommand this function executes a given command.
func ExecuteCommand(command string, args []string) (string, string, error) {

	cmd := exec.Command(command, args...)
	stderr, _ := cmd.StderrPipe()
	stdout, _ := cmd.StdoutPipe()

	err := cmd.Start()

	scannerErr := bufio.NewScanner(stderr)
	errStr := ""
	for scannerErr.Scan() {
		errStr += scannerErr.Text() + "\n"
	}

	scannerOut := bufio.NewScanner(stdout)
	outStr := ""
	for scannerOut.Scan() {
		outStr += scannerOut.Text() + "\n"
	}

	if len(outStr) > 0 {
		outStr = outStr[:len(outStr)-1]
	}
	if len(errStr) > 0 {
		errStr = errStr[:len(errStr)-1]
	}

	err = cmd.Wait()
	return outStr, errStr, err

}

//ExecuteTwoCmdsWithPipe this function executes a given command.
func ExecuteTwoCmdsWithPipe(cmd1 []string, cmd2 []string) (string, error) {

	//create command
	command1 := exec.Command(cmd1[0], cmd1[1:]...)
	command2 := exec.Command(cmd2[0], cmd2[1:]...)

	//make a pipe
	reader, writer := io.Pipe()
	var buf bytes.Buffer

	//set the output of "cat" command to pipe writer
	command1.Stdout = writer
	//set the input of the "wc" command pipe reader

	command2.Stdin = reader

	//cache the output of "wc" to memory
	command2.Stdout = &buf

	//start to execute "cat" command
	command1.Start()

	//start to execute "wc" command
	command2.Start()

	//waiting for "cat" command complete and close the writer
	command1.Wait()
	writer.Close()

	//waiting for the "wc" command complete and close the reader
	command2.Wait()
	reader.Close()
	//copy the buf to the standard output

	strBuf := new(strings.Builder)
	_, err := io.Copy(strBuf, &buf)
	if err != nil {
		return "", err
	}

	return strBuf.String(), nil

}

//FileExists will check if a file (not directory) exists in the specified path.
func FileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

//InSlice will check if an element exists in the slice
func InSlice(slice []string, val string) (int, bool) {
	for i, item := range slice {
		if item == val {
			return i, true
		}
	}
	return -1, false
}

//WriteToTempFile writes the contents of the string to a temp file
func WriteToTempFile(content string) (string, error) {

	tmpFile, err := ioutil.TempFile(os.TempDir(), "densify-")

	text := []byte(content)
	if _, err = tmpFile.Write(text); err != nil {
		return tmpFile.Name(), err
	}

	// Close the file
	if err := tmpFile.Close(); err != nil {
		return tmpFile.Name(), err
	}

	return tmpFile.Name(), nil

}

//DeleteFile deletes a file specified in the path
func DeleteFile(filepath string) error {

	err := os.Remove(filepath)
	if err != nil {
		return err
	}

	return nil

}
