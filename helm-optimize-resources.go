package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	"github.com/densify-quick-start/helm-optimize-resources/densify"
	"github.com/densify-quick-start/helm-optimize-resources/ssm"
	"github.com/densify-quick-start/helm-optimize-resources/support"
	"github.com/ghodss/yaml"
)

var availableAdapters = [][]string{{"densify", "Densify"}, {"ssm", "Parameter Store"}}
var adapter string
var cluster string
var namespace string
var supportedObjTypes = []string{"CronJob", "DaemonSet", "Job", "ReplicaSet", "ReplicationController", "StatefulSet", "Deployment", "Pod"}

//HelmBin location of helm installation
var HelmBin string

func printHowToUse() error {

	content, err := ioutil.ReadFile(os.Getenv("HELM_PLUGIN_DIR") + "/plugin.yaml")
	support.CheckError("", err, true)

	var pluginYAML map[string]interface{}
	yaml.Unmarshal(content, &pluginYAML)
	fmt.Println("----------------------------------------------------")
	fmt.Println("NAME: Optimize Plugin")
	fmt.Println("VERSION: " + pluginYAML["version"].(string))
	fmt.Println("----------------------------------------------------")
	fmt.Println(pluginYAML["description"].(string))
	fmt.Println("----------------------------------------------------")

	return nil

}

func initializeAdapter() error {

	var err error
	adapter, err = support.RetrieveStoredSecret("optimize-plugin-secrets", "adapter")
	if err != nil {
		configAdapter()
	}

	switch adapter {
	case "densify":
		err = densify.Initialize()
	case "ssm":
		err = ssm.Initialize()
	}

	if err != nil {
		fmt.Println(err)
		support.RemoveSecret("optimize-plugin-secrets")
		var tryAgain string
		fmt.Print("Would you like to try again (y/n): ")
		fmt.Scanln(&tryAgain)
		if tryAgain == "y" {
			return initializeAdapter()
		}
	}

	return err

}

func getInsight(cluster string, namespace string, objType string, objName string, containerName string) (map[string]map[string]string, error) {

	var insight map[string]map[string]string
	var err error

	switch adapter {
	case "densify":
		insight, err = densify.GetInsight(cluster, namespace, objType, objName, containerName)
	case "ssm":
		insight, err = ssm.GetInsight(cluster, namespace, objType, objName, containerName)
	}

	if err != nil {
		return nil, err
	}

	return insight, nil

}

func configAdapter() {

	//get adapter selection from user
	for {
		fmt.Println("Select Adapter")
		for i, val := range availableAdapters {
			fmt.Println("  " + strconv.Itoa(i+1) + ". " + val[1])
		}
		fmt.Print("Selection: ")

		var selectedValue string
		fmt.Scanln(&selectedValue)

		var userSelection int
		var err error
		if userSelection, err = strconv.Atoi(selectedValue); err != nil || (userSelection < 1 || userSelection > len(availableAdapters)) {
			fmt.Println("Incorrect adapter selection.  Try again.")
			continue
		}
		adapter = availableAdapters[userSelection-1][0]
		break
	}

}

func processPluginSwitches(args []string) {

	//Check if user requesting help
	if len(args) == 0 || args[0] == "-h" || args[0] == "help" || args[0] == "--help" {
		printHowToUse()
		os.Exit(0)
	}

	//Check if user is configuring plugin
	if args[0] == "-c" || args[0] == "--configure" {
		support.RemoveSecret("optimize-plugin-secrets")
		initializeAdapter()
		os.Exit(0)
	}

}

func main() {

	//set environment variables
	HelmBin = os.Getenv("HELM_BIN")

	args := os.Args[1:]

	//Check general dependancies
	if _, _, err := support.ExecuteSingleCommand([]string{"kubectl"}); err != nil {
		fmt.Println("kubectl is not installed, not in path or not configured correctly")
		os.Exit(0)
	}

	//Process any plugin switches
	processPluginSwitches(args)

	//initialize the adapter
	if err := initializeAdapter(); err != nil {
		fmt.Println(err)
		os.Exit(0)
	}

	//if helm command is not install, upgrade or template, then just pass along to helm.
	if args[0] != "install" && args[0] != "upgrade" {

		stdOut, stdErr, err := support.ExecuteSingleCommand(append([]string{HelmBin}, args...))
		support.CheckError(stdErr, err, true)
		fmt.Println(stdOut)
		os.Exit(0)

	} else {

		fmt.Println("--------------------------------------------------------------------------------------------------------------------------------")

		interpolateContext()
		fmt.Println("CLUSTER: " + cluster)
		fmt.Println("ADAPTER: " + adapter)

		absChartPath, _ := filepath.Abs(args[2])
		chartDirName := filepath.Base(absChartPath)

		//create temporary chart directory
		tempChartDir, err := ioutil.TempDir("", "")
		support.CheckError("", err, true)

		//check to see if valid chart directory passed in.
		//if not pull from repo
		if support.FileExists(args[2] + "/Chart.yaml") {
			_, stdErr, err := support.ExecuteSingleCommand([]string{"cp", "-a", absChartPath, tempChartDir})
			support.CheckError(stdErr, err, true)
		} else {
			_, stdErr, err := support.ExecuteSingleCommand([]string{HelmBin, "pull", args[2], "--untar", "--untardir", tempChartDir})
			support.CheckError(stdErr, err, true)
		}

		//render chart and output to temporary directory
		_, stdErr, err := support.ExecuteSingleCommand(append(append([]string{HelmBin, "template"}, args[1:]...), "--output-dir", tempChartDir))
		support.CheckError(stdErr, err, true)

		processChart(tempChartDir+"/"+chartDirName, args)

		args[2] = tempChartDir + "/" + chartDirName
		stdOut, stdErr, err := support.ExecuteSingleCommand(append([]string{HelmBin}, args...))
		support.CheckError(stdErr, err, true)

		//delete temporary chart directory
		_, stdErr, err = support.ExecuteSingleCommand([]string{"rm", "-rf", tempChartDir})
		support.CheckError(stdErr, err, true)

		fmt.Println("\n--------------------------------------------------------------------------------------------------------------------------------")
		fmt.Println(stdOut)

	}
}

func processChart(chartPath string, args []string) error {

	charts, err := ioutil.ReadDir(chartPath + "/charts")
	if err == nil {
		for _, chart := range charts {
			if chart.IsDir() {
				processChart(chartPath+"/charts/"+chart.Name(), args)
			}
		}
	}

	templates, err := ioutil.ReadDir(chartPath + "/templates")
	if err != nil {
		fmt.Println(err)
	}

	for _, template := range templates {
		if !template.IsDir() {

			manifest, err := ioutil.ReadFile(chartPath + "/templates/" + template.Name())
			support.CheckError("", err, true)

			var manifestYAML map[string]interface{}
			if err := yaml.Unmarshal([]byte(manifest), &manifestYAML); err != nil {
				continue
			}

			objType := manifestYAML["kind"].(string)
			objName := manifestYAML["metadata"].(map[string]interface{})["name"].(string)

			if _, ok := support.InSlice(supportedObjTypes, objType); !ok {
				continue
			}

			objNamespace := namespace
			if val, ok := manifestYAML["metadata"].(map[string]interface{})["namespace"]; ok && val != nil {
				objNamespace = manifestYAML["metadata"].(map[string]interface{})["namespace"].(string)
			}

			var containers []interface{}
			switch objType {
			case "Pod":
				containers = manifestYAML["spec"].(map[string]interface{})["containers"].([]interface{})
			case "CronJob":
				containers = manifestYAML["spec"].(map[string]interface{})["jobTemplate"].(map[string]interface{})["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
			default:
				containers = manifestYAML["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
			}

			fmt.Println("\nnamespace[" + objNamespace + "] objType[" + objType + "] objName[" + objName + "]")
			var i int = 1
			for _, container := range containers {
				var containerName string
				var defaultConfig map[string]interface{} = nil
				if val, ok := container.(map[string]interface{})["name"]; ok && val != nil {
					containerName = container.(map[string]interface{})["name"].(string)
				}
				if val, ok := container.(map[string]interface{})["resources"]; ok && val != nil {
					defaultConfig = container.(map[string]interface{})["resources"].(map[string]interface{})
				}
				fmt.Println(strconv.Itoa(i) + "." + containerName)
				fmt.Print("  Checking Repo: ")
				insight, err := getInsight(cluster, objNamespace, objType, objName, containerName)
				if err != nil {
					fmt.Println(err)
					fmt.Print("  Checking Cluster: ")
					insight, err = extractResourceSpecFromK8S(cluster, objNamespace, objType, objName, containerName)
					if err != nil {
						fmt.Println(err)
						fmt.Print("  Checking Defaults: ")
						if defaultConfig != nil {
							fmt.Println(defaultConfig)
						} else {
							fmt.Println("*WARNING* No default config present!")
						}
					}
				}
				if insight != nil {
					fmt.Println(insight)
					container.(map[string]interface{})["resources"] = insight
				}
				i++
			}

			manifestYAMLStr, err := yaml.Marshal(manifestYAML)
			support.CheckError("", err, true)
			err = ioutil.WriteFile(chartPath+"/templates/"+template.Name(), manifestYAMLStr, 0644)

		}
	}

	return nil

}

func extractResourceSpecFromK8S(cluster string, objNamespace string, objType string, objName string, containerName string) (map[string]map[string]string, error) {

	var jsonPath string
	switch objType {

	case "Pod":
		jsonPath = "{.spec.containers}"
	case "CronJob":
		jsonPath = "{.spec.jobTemplate.spec.template.spec.containers}"
	default:
		jsonPath = "{.spec.template.spec.containers}"

	}

	stdOut, stdErr, err := support.ExecuteSingleCommand([]string{"kubectl", "get", objType, objName, "-o=jsonpath=" + jsonPath, "--cluster=" + cluster, "--namespace=" + objNamespace})
	if err != nil {
		return nil, errors.New(stdErr)
	}

	var containerDefs []map[string]interface{}
	json.Unmarshal([]byte(stdOut), &containerDefs)

	for _, containerDef := range containerDefs {
		if containerName == containerDef["name"].(string) {
			if _, ok := containerDef["resources"]; !ok {
				break
			}

			jsonStr, err := json.Marshal(containerDef["resources"])
			if err != nil || string(jsonStr) == "{}" {
				break
			}

			var parsedInsight map[string]map[string]string
			json.Unmarshal([]byte(jsonStr), &parsedInsight)

			return parsedInsight, nil
		}
	}

	return nil, errors.New("could not find resource spec")

}

func interpolateContext() {

	//extract working context-info (cluster and namespace)
	kubeconfig := os.Getenv("KUBECONFIG")
	kubecontext := os.Getenv("HELM_KUBECONTEXT")
	namespace = os.Getenv("HELM_NAMESPACE")

	var stdErr string
	var err error
	if kubeconfig != "" {
		kubeconfig, stdErr, err = support.ExecuteSingleCommand([]string{"cat", kubeconfig})
	} else {
		kubeconfig, stdErr, err = support.ExecuteSingleCommand([]string{"kubectl", "config", "view"})
	}
	support.CheckError(stdErr, err, true)

	var kubeconfigYAML map[string]interface{}
	err = yaml.Unmarshal([]byte(kubeconfig), &kubeconfigYAML)
	support.CheckError("", err, true)

	//determine current-context
	if kubecontext == "" {
		kubecontext = kubeconfigYAML["current-context"].(string)
	}

	//determine cluster
	contextList := kubeconfigYAML["contexts"].([]interface{})
	for _, context := range contextList {
		if context.(map[string]interface{})["name"] == kubecontext {
			cluster = context.(map[string]interface{})["context"].(map[string]interface{})["cluster"].(string)
		}
	}

}
