package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	densify "./densify"
	ssm "./ssm"
	support "./support"
	"github.com/ghodss/yaml"
)

var adapter string

func printHowToUse() error {

	content, err := ioutil.ReadFile(os.Getenv("HELM_PLUGIN_DIR") + "/plugin.yaml")
	support.CheckErr("", err)

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

func initializeAdapter(create bool) {

	var err error

	switch adapter {
	case "densify":
		err = densify.Initialize(create)
	case "ssm":
		err = ssm.Initialize(create)
	}

	if err != nil {
		os.Exit(0)
	}

}

func getInsight(cluster string, namespace string, objType string, objName string, containerName string) (string, error) {

	var insight string
	var err error

	switch adapter {
	case "densify":
		insight, err = densify.GetInsight(cluster, namespace, objType, objName, containerName)
	case "ssm":
		insight, err = ssm.GetInsight(cluster, namespace, objType, objName, containerName)
	}

	if err != nil {
		return "", err
	}

	return insight, nil

}

func main() {

	args := os.Args[1:]

	//Check general dependancies
	stdOut, stdErr, err := support.ExecuteSingleCommand([]string{"whereis", "kubectl"})
	support.CheckErr(stdErr, err)
	if !strings.Contains(strings.Split(stdOut, ":")[1], "kubectl") {
		fmt.Println("kubectl is not available.  please install before trying again.")
		os.Exit(0)
	}

	//Check if user requesting help
	if len(args) == 0 || args[0] == "-h" || args[0] == "help" || args[0] == "--help" {
		printHowToUse()
		os.Exit(0)
	}

	//Identify configured adapter
	adapter, err = support.RetrieveStoredSecret("optimize-plugin-secrets", "adapter")

	//Check if user is configuring plugin
	if args[0] == "-c" || args[0] == "--configure" || err != nil {

		//get adapter selection from user
		for {
			fmt.Print("Which adapter would you like to use (ssm/densify) [densify]: ")
			fmt.Scanln(&adapter)
			if adapter == "" {
				adapter = "densify"
			} else if adapter != "densify" && adapter != "ssm" {
				fmt.Println("Incorrect adapter selection.  Try again.")
				continue
			}
			break
		}

		initializeAdapter(true)
		os.Exit(0)

	}

	//if helm command is not install, upgrade or template, then just pass along to helm.
	if args[0] != "install" && args[0] != "upgrade" && args[0] != "template" {

		stdOut, stdErr, err := support.ExecuteSingleCommand(append([]string{"helm"}, args...))
		support.CheckErr(stdErr, err)
		fmt.Println(stdOut)
		os.Exit(0)

	} else {

		fmt.Println("--------------------------------------------------------------------------------------------------------------------------------")

		initializeAdapter(false)
		cluster, namespace := interpolateContext()

		//Generate a list of values and options
		valueFiles := []string{strings.TrimRight(args[2], "/") + "/values.yaml"}
		options := []string{}
		for i, s := range args {
			if (s == "-f" || s == "--values") && len(args) > i+1 && support.FileExists(args[i+1]) {
				valueFiles = append(valueFiles, args[i+1])
			} else if _, exists := support.InSlice(valueFiles, s); !exists {
				options = append(options, args[i])
			}
		}

		//Generate single concatenated values file
		var valueFilesConcatenatedStr string
		for _, valueFile := range valueFiles {
			fileData, err := ioutil.ReadFile(valueFile)
			support.CheckErr("", err)
			valueFilesConcatenatedStr += string(fileData) + "\n"
		}

		//replace keywords
		i := 0
		for {
			valueFilesConcatenatedStr = strings.Replace(valueFilesConcatenatedStr, "{{optimize}}", "{{optimize_"+strconv.Itoa(i)+"}}", 1)
			if strings.Contains(valueFilesConcatenatedStr, "{{optimize}}") == false {
				break
			}
			i++
		}

		//create temp file to hold concatenated value file
		filename, err := support.WriteToTempFile(valueFilesConcatenatedStr)
		if err != nil {
			fmt.Println(err)
		}

		//create a template command based on user's original command
		templateCmd := make([]string, len(options))
		copy(templateCmd, options)
		templateCmd[0] = "template"

		//cat the temp file and pipe it to the template command
		stdOut, err := support.ExecuteTwoCmdsWithPipe([]string{"cat", filename}, append(append([]string{"helm"}, templateCmd...), []string{"--dry-run", "-f", "-"}...))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		//delete temp file
		if err := support.DeleteFile(filename); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		//convert concatenated values file into a json string
		valueFilesJSONByteArray, err := yaml.YAMLToJSON([]byte(valueFilesConcatenatedStr))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		valueFilesJSONStr := string(valueFilesJSONByteArray)

		//analyze each of the manifests separately
		i = 1
		manifests := strings.Split(stdOut, "---")
		for _, s := range manifests {
			if strings.Contains(s, "{{optimize") {
				objNamespace, objType, objName, containers := extractObjKeysFromHelm(s)
				support.CheckErr("", err)

				if objNamespace == "" {
					objNamespace = namespace
				}

				fmt.Println(strconv.Itoa(i) + ". Processing cluster[" + cluster + "] namespace[" + objNamespace + "] objType[" + objType + "] objName[" + objName + "]")
				i++

				for containerName, key := range containers {
					fmt.Print("   " + containerName + ": ")
					insightJSONStr, err := getInsight(cluster, objNamespace, objType, objName, containerName)
					if err != nil {
						fmt.Println(err)
						if options[0] == "install" {
							fmt.Print("   Install request, returning empty resources: ")
							insightJSONStr = "{}"
						} else {
							fmt.Print("   Current resource specs: ")
							insightJSONStr, err = extractResourceSpecFromK8S(cluster, objNamespace, objType, objName, containerName)
							support.CheckErr("", err)
						}
					}
					fmt.Println(insightJSONStr)
					valueFilesJSONStr = strings.ReplaceAll(valueFilesJSONStr, "\""+key+"\"", insightJSONStr)
				}
			}
		}

		//convert JSON back to YAML
		valueFileYAMLByteArray, err := yaml.JSONToYAML([]byte(valueFilesJSONStr))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		valueFilesYAMLStr := string(valueFileYAMLByteArray)

		//create temp file to hold concatenated value file
		filename, err = support.WriteToTempFile(valueFilesYAMLStr)
		if err != nil {
			fmt.Println(err)
		}

		//cat the temp file and pipe it to the users helm command
		stdOut, err = support.ExecuteTwoCmdsWithPipe([]string{"cat", filename}, append(append([]string{"helm"}, options...), []string{"-f", "-"}...))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("--------------------------------------------------------------------------------------------------------------------------------")
		fmt.Println(stdOut)
		//delete temp file
		if err := support.DeleteFile(filename); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

	}
}

func extractObjKeysFromHelm(manifest string) (string, string, string, map[string]string) {

	var manifestYAML map[string]interface{}
	if err := yaml.Unmarshal([]byte(manifest), &manifestYAML); err != nil {
		support.CheckErr("", err)
	}

	namespace := ""
	_, ok := manifestYAML["metadata"].(map[string]interface{})["namespace"]
	if ok {
		namespace = manifestYAML["metadata"].(map[string]interface{})["namespace"].(string)
	}

	objType := manifestYAML["kind"].(string)
	objName := manifestYAML["metadata"].(map[string]interface{})["name"].(string)
	containerNames := make(map[string]string)

	containers := manifestYAML["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
	for _, container := range containers {
		containerStr, err := yaml.Marshal(&container)
		if err != nil {
			support.CheckErr("", err)
		}
		if strings.Contains(string(containerStr), "{{optimize") {
			containerNames[container.(map[string]interface{})["name"].(string)] = container.(map[string]interface{})["resources"].(string)
		}
	}

	return namespace, objType, objName, containerNames

}

func extractResourceSpecFromK8S(cluster string, namespace string, objType string, objName string, containerName string) (string, error) {

	var jsonPath string
	switch objType {

	case "Pod":
		jsonPath = "{.spec.containers}"
	case "CronJob":
		jsonPath = "{.spec.jobTemplate.spec.template.spec.containers}"
	default:
		//this will capture DaemonSet, Deployment, ReplicaSet, ReplicationController, Job, StatefulSet
		jsonPath = "{.spec.template.spec.containers}"

	}

	stdOut, stdErr, err := support.ExecuteCommand("kubectl", []string{"get", objType, objName, "-o=jsonpath=" + jsonPath, "--cluster=" + cluster, "--namespace=" + namespace})
	if err != nil {
		fmt.Println(stdErr)
		return "", err
	}

	var containerDefs []map[string]interface{}
	json.Unmarshal([]byte(stdOut), &containerDefs)

	for _, containerDef := range containerDefs {
		if containerName == containerDef["name"].(string) {
			jsonBytes, err := json.Marshal(containerDef["resources"].(map[string]interface{}))
			if err != nil {
				return "", err
			}
			return string(jsonBytes), nil
		}
	}

	return "", errors.New("could not find container spec")

}

func interpolateContext() (string, string) {

	//extract working context-info (cluster and namespace)
	kubeconfig := os.Getenv("KUBECONFIG")
	kubecontext := os.Getenv("HELM_KUBECONTEXT")
	namespace := os.Getenv("HELM_NAMESPACE")

	var stdErr string
	var err error
	if kubeconfig != "" {
		kubeconfig, stdErr, err = support.ExecuteSingleCommand([]string{"cat", kubeconfig})
	} else {
		kubeconfig, stdErr, err = support.ExecuteSingleCommand([]string{"kubectl", "config", "view"})
	}
	support.CheckErr(stdErr, err)

	var kubeconfigYAML map[string]interface{}
	err = yaml.Unmarshal([]byte(kubeconfig), &kubeconfigYAML)
	support.CheckErr("", err)

	//determine current-context
	if kubecontext == "" {
		kubecontext = kubeconfigYAML["current-context"].(string)
	}

	//determine cluster
	var cluster string
	contextList := kubeconfigYAML["contexts"].([]interface{})
	for _, context := range contextList {
		if context.(map[string]interface{})["name"] == kubecontext {
			cluster = context.(map[string]interface{})["context"].(map[string]interface{})["cluster"].(string)
		}
	}

	return cluster, namespace

}

/*
kubectl config view
--> get current cluster and namespace

kubectl get pods environment-deployment-956567769-7hlz7 -o=jsonpath='{.spec.containers[0].resources}'cd
kubectl get pods environment-deployment-956567769-7hlz7 -o=jsonpath='{.spec.containers}'


kubectl get all --all-namespaces -l='app.kubernetes.io/managed-by=Helm,app.kubernetes.io/instance=my-cherry-chart'
--> this command will get all k8s objects deployed by a specific helm chart.

Process to extract ObjType, ObjName, ContainerName
1) Generate a concatenated list of value files.
2) For all instances of "{{lookup_resources}}" replace with "{{lookup_resources_x}}", where x is an Int
3) Run Helm template command and pipe in updated value files
   --> cat buildachart/values.yaml | helm template my-cherry-chart buildachart/ -f -
4) Split the outputted text by "---"
5) Locate the "{{lookup_resources_x}}" identifiers and read ObjType, ObjName and ContainerName it belongs to.
6) Create a resources map of "{{lookup_resources_x}}" --> json_str_resources.
7) For each one identified, perform a lookup in Densify and add to resources map.
8) Update step 2 json with the values in the resources map.
9) convert to yaml

*/
