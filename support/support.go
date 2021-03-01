package support

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

//CheckError
func CheckError(message string, err error, exit bool) bool {
	if err != nil {
		if message != "" {
			fmt.Println(message)
		}
		fmt.Println(err)
		if exit {
			os.Exit(1)
		}
		return true
	}
	return false
}

//RemoveSecret deletes the specified k8s secret
func RemoveSecret(name string) {
	_, _, _ = ExecuteSingleCommand([]string{"kubectl", "delete", "secret", name, "--ignore-not-found"})
}

//RetrieveStoredSecret extract a secret stored within K8S.  Only in working namespace.
func RetrieveStoredSecret(secret string, name string) (string, error) {

	valueEncoded, stdErr, err := ExecuteSingleCommand([]string{"kubectl", "get", "secret", secret, "-o", "jsonpath='{.data." + name + "}'"})
	if err != nil {
		return "", errors.New(stdErr)
	}

	valueEncoded = valueEncoded[1 : len(valueEncoded)-1]
	valueDecoded, err := base64.StdEncoding.DecodeString(valueEncoded)
	if err != nil {
		return "", err
	}

	return string(valueDecoded), nil

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

//LinuxCommandExists checks to see if the linux command is available via command line
func LinuxCommandExists(command string) bool {

	stdOut, stdErr, err := ExecuteSingleCommand([]string{"whereis", command})
	CheckError(stdErr, err, false)
	if !strings.Contains(strings.Split(stdOut, ":")[1], command) {
		return false
	}
	return true

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

//HttpRequest send a REST api request to an end point
func HttpRequest(method string, endpoint string, authStr string, body []byte) (string, error) {

	auth := base64.StdEncoding.EncodeToString([]byte(authStr))
	req, _ := http.NewRequest(method, endpoint, bytes.NewBuffer(body))
	req.Header.Add("Authorization", "Basic "+auth)
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Accept", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode == 200 {
		return string(bodyBytes), nil
	} else {
		return "", errors.New(string(bodyBytes))
	}

}
