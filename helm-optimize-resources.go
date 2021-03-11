package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/densify-quick-start/helm-optimize-resources/densify"
	"github.com/densify-quick-start/helm-optimize-resources/ssm"
	"github.com/densify-quick-start/helm-optimize-resources/support"
	"github.com/ghodss/yaml"
)

//VARIABLE DECLARATIONS
var availableAdapters = map[int]string{
	1: "Densify",
	2: "Parameter Store",
}
var adapter string
var localCluster string
var remoteCluster string
var namespace string
var objTypeContainerPath = map[string]string{
	"Pod":                   "{.spec.containers}",
	"CronJob":               "{.spec.jobTemplate.spec.template.spec.containers}",
	"DaemonSet":             "{.spec.template.spec.containers}",
	"Job":                   "{.spec.template.spec.containers}",
	"ReplicaSet":            "{.spec.template.spec.containers}",
	"ReplicationController": "{.spec.template.spec.containers}",
	"StatefulSet":           "{.spec.template.spec.containers}",
	"Deployment":            "{.spec.template.spec.containers}",
}

//HelmBin location of helm installation
var HelmBin string = os.Getenv("HELM_BIN")

//KubectlBin location of kubectl installation
var KubectlBin string = "kubectl"

////////////////////////////////////////////////////////
/////////////////ADAPTER FUNCTIONS//////////////////////
////////////////////////////////////////////////////////

func initializeAdapter() error {

	if adapter == "" {
		if val, ok := support.RetrieveSecrets("helm-optimize-plugin")["adapter"]; ok {
			adapter = val
		} else if adapter == "" {
			adapter = "Densify"
		}
	}

	var err error
	switch adapter {
	case "Densify":
		err = densify.Initialize()
	case "Parameter Store":
		err = ssm.Initialize()
	}

	if err != nil {
		fmt.Println(err)
		var tryAgain string
		fmt.Print("Would you like to try again (y/n): ")
		fmt.Scanln(&tryAgain)
		if tryAgain == "y" {
			return initializeAdapter()
		}
	}

	return err

}

func getInsight(cluster string, namespace string, objType string, objName string, containerName string) (map[string]map[string]string, string, error) {

	var insight map[string]map[string]string
	var approvalSetting string
	var err error

	switch adapter {
	case "Densify":
		insight, approvalSetting, err = densify.GetInsight(cluster, namespace, objType, objName, containerName)
	case "Parameter Store":
		insight, approvalSetting, err = ssm.GetInsight(cluster, namespace, objType, objName, containerName)
	}

	if err != nil {
		return nil, "Not Approved", err
	}

	return insight, approvalSetting, nil

}

func updateApprovalSetting(approved bool, cluster string, namespace string, objType string, objName string, containerName string) error {

	var err error

	switch adapter {
	case "Densify":
		err = densify.UpdateApprovalSetting(approved, cluster, namespace, objType, objName, containerName)
	case "Parameter Store":
		err = ssm.UpdateApprovalSetting(approved, cluster, namespace, objType, objName, containerName)
	}

	return err

}

func getApprovalSetting(cluster string, namespace string, objType string, objName string, containerName string) (string, error) {

	var approvalSetting string
	var err error

	switch adapter {
	case "Densify":
		approvalSetting, err = densify.GetApprovalSetting(cluster, namespace, objType, objName, containerName)
	case "Parameter Store":
		approvalSetting, err = ssm.GetApprovalSetting(cluster, namespace, objType, objName, containerName)
	}

	return approvalSetting, err

}

////////////////////////////////////////////////////////
/////////////////SUPPORTING FUNCTIONS///////////////////
////////////////////////////////////////////////////////

func selectAdapter() {

	//get adapter selection from user
	for {
		fmt.Println("Select Adapter")
		i := 1
		for range availableAdapters {
			fmt.Println("  " + strconv.Itoa(i) + ". " + availableAdapters[i])
			i++
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
		adapter = availableAdapters[userSelection]
		break
	}

}

func processPluginSwitches(args []string) {

	//Check if user requesting help
	if len(args) == 0 || args[0] == "-h" || args[0] == "help" || args[0] == "--help" {
		printHowToUse()
		os.Exit(0)
	}

	if args[0] == "-c" && len(args) == 2 {
		//Check if user is configuring adapter
		if args[1] == "--adapter" {
			selectAdapter()
			initializeAdapter()
			os.Exit(0)
		}

		//Check if user is configuring adapter
		if args[1] == "--cluster-mapping" {
			remoteCluster = ""
			fmt.Print("Please specify remote cluster [" + localCluster + "]: ")
			fmt.Scanln(&remoteCluster)
			if remoteCluster == "" {
				remoteCluster = localCluster
			}
			support.StoreSecrets("helm-optimize-plugin", map[string]string{"remoteCluster": remoteCluster})
			os.Exit(0)
		}
	}

	if args[0] == "-a" && len(args) > 2 {

		if err := initializeAdapter(); err != nil {
			os.Exit(0)
		}

		stdOut, stdErr, err := support.ExecuteSingleCommand(append([]string{HelmBin, "template"}, args...))
		support.CheckError(stdErr, err, true)

		fmt.Println("LOCAL CLUSTER: " + localCluster)
		fmt.Println("REMOTE CLUSTER: " + remoteCluster)
		fmt.Println("ADAPTER: " + adapter)

		for _, manifest := range strings.Split(stdOut, "---") {

			var manifestMap map[string]interface{}
			yaml.Unmarshal([]byte(manifest), &manifestMap)

			if _, ok := manifestMap["metadata"]; ok {
				if _, ok := manifestMap["metadata"].(map[string]interface{})["annotations"]; ok {
					if helmTest, ok := manifestMap["metadata"].(map[string]interface{})["annotations"].(map[string]interface{})["helm.sh/hook"]; ok && strings.HasPrefix(helmTest.(string), "test") {
						continue
					}
				}
			}

			if objType, ok := manifestMap["kind"]; ok {

				if _, ok := objTypeContainerPath[objType.(string)]; ok {

					objName := manifestMap["metadata"].(map[string]interface{})["name"].(string)
					var objNamespace string
					if ns, ok := manifestMap["metadata"].(map[string]interface{})["namespace"]; ok {
						objNamespace = ns.(string)
					} else {
						objNamespace = namespace
					}

					var containers []interface{}
					if objType == "Pod" {
						containers = manifestMap["spec"].(map[string]interface{})["containers"].([]interface{})
					} else if objType == "CronJob" {
						containers = manifestMap["spec"].(map[string]interface{})["jobTemplate"].(map[string]interface{})["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
					} else {
						containers = manifestMap["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{})["containers"].([]interface{})
					}

					fmt.Println("\nnamespace[" + objNamespace + "] objType[" + objType.(string) + "] objName[" + objName + "]")
					for i, container := range containers {

						containerName := container.(map[string]interface{})["name"].(string)
						approvalSetting, err := getApprovalSetting(remoteCluster, objNamespace, objType.(string), objName, containerName)
						if err != nil {
							fmt.Println(strconv.Itoa(i+1) + "." + containerName + " not found in repository.")
							continue
						}
						fmt.Print(strconv.Itoa(i+1) + "." + containerName + " (" + approvalSetting + ") ")
						var approval string
						if approvalSetting == "Not Approved" {
							fmt.Print("Approve this insight (y/n) [y]: ")
							fmt.Scanln(&approval)
							if approval == "y" || approval == "" {
								updateApprovalSetting(true, remoteCluster, namespace, objType.(string), objName, containerName)
							}
						} else {
							fmt.Print("Unapprove this insight (y/n) [y]: ")
							fmt.Scanln(&approval)
							if approval == "y" || approval == "" {
								updateApprovalSetting(false, remoteCluster, namespace, objType.(string), objName, containerName)
							}
						}

					}

				}

			}

		}
		os.Exit(0)

	}

	//Check for errors
	if args[0] == "-c" || args[0] == "-a" {
		fmt.Println("incorrect optimize-plugin command - refer to help menu")
		os.Exit(0)
	}

}

func printHowToUse() error {

	content, err := ioutil.ReadFile(os.Getenv("HELM_PLUGIN_DIR") + "/plugin.yaml")
	support.CheckError("", err, true)

	var pluginYAML map[string]interface{}
	yaml.Unmarshal(content, &pluginYAML)
	fmt.Println("--------------------------------------------------------------------------------------------------------------------------------")
	fmt.Println("NAME: Optimize Plugin")
	fmt.Println("VERSION: " + pluginYAML["version"].(string))
	fmt.Println("--------------------------------------------------------------------------------------------------------------------------------")
	fmt.Println(pluginYAML["description"].(string))
	fmt.Println("--------------------------------------------------------------------------------------------------------------------------------")

	return nil

}

func checkGeneralDependancies() {

	for {
		if _, _, err := support.ExecuteSingleCommand([]string{KubectlBin}); err != nil {
			fmt.Print("[" + KubectlBin + "] is not available -- enter new path: ")
			fmt.Scanln(&KubectlBin)
			fmt.Println("")
		} else {
			if stdOut, stdErr, err := support.ExecuteSingleCommand([]string{KubectlBin, "cluster-info"}); err != nil {
				fmt.Println(stdOut)
				fmt.Println(stdErr)
				os.Exit(0)
			}
			support.KubectlBin = KubectlBin
			break
		}
	}

}

func scanFlagsForChartDetails(args []string) (string, int, error) {

	if !strings.HasPrefix(args[1], "-") && !strings.HasPrefix(args[2], "-") {
		return args[2], 2, nil
	}

	stdOut, stdErr, err := support.ExecuteSingleCommand([]string{HelmBin, args[0], "-h"})
	if err != nil {
		return "", 0, errors.New(stdErr)
	}

	re := regexp.MustCompile("Flags:(.*\n)+")
	match := re.FindStringSubmatch(stdOut)

	re2 := regexp.MustCompile(`\r?\n`)
	match2 := re2.ReplaceAllString(match[0], " ")

	re3 := regexp.MustCompile("(--[a-zA-Z]*([-]{0,1}[a-zA-Z]*)*   )+")
	match3 := re3.FindAllString(match2, -1)

	var flags []string
	for _, val := range match3 {
		flag := strings.Trim(val, " ")
		flags = append(flags, flag)
		re4 := regexp.MustCompile("-[a-z], " + flag)
		match4 := re4.FindAllString(match2, -1)
		if len(match4) > 0 {
			flags = append(flags, strings.Split(match4[0], ",")[0])
		}
	}

	argstemp := args[1:]

	chart := ""
	chartPos := 1
	relFound := false

	for i, arg := range argstemp {

		if !strings.HasPrefix(arg, "-") {

			if i == 0 {
				if !relFound {
					relFound = true
					continue
				} else {
					chart = arg
					chartPos += i
					break
				}
			} else if i == 1 {
				if _, ok := support.InSlice(flags, argstemp[i-1]); ok || !strings.HasPrefix(argstemp[i-1], "-") {
					if !relFound {
						relFound = true
						continue
					} else {
						chart = arg
						chartPos += i
						break
					}
				}
			} else {
				if strings.HasPrefix(argstemp[i-1], "-") {
					if _, ok := support.InSlice(flags, argstemp[i-1]); ok {
						if !relFound {
							relFound = true
							continue
						} else {
							chart = arg
							chartPos += i
							break
						}
					}
				} else {
					if !relFound {
						relFound = true
						continue
					} else {
						chart = arg
						chartPos += i
						break
					}
				}
			}
		}
	}

	return chart, chartPos, nil

}

func main() {

	//set environment variables
	args := os.Args[1:]

	checkGeneralDependancies()
	interpolateContext()
	processPluginSwitches(args)

	//initialize the adapter
	if adapter == "" {
		if err := initializeAdapter(); err != nil {
			os.Exit(0)
		}
	}

	//if helm command is not install, upgrade or template, then just pass along to helm.
	if args[0] != "install" && args[0] != "upgrade" {

		stdOut, stdErr, err := support.ExecuteSingleCommand(append([]string{HelmBin}, args...))
		support.CheckError(stdErr, err, true)
		fmt.Println(stdOut)
		os.Exit(0)

	} else {

		//validate whether the command is legal
		_, stdErr, err := support.ExecuteSingleCommand(append(append([]string{HelmBin}, args...), "--dry-run"))
		support.CheckError(stdErr, err, true)

		chart, argPos, err := scanFlagsForChartDetails(os.Args[1:])
		support.CheckError("", err, true)

		fmt.Println("--------------------------------------------------------------------------------------------------------------------------------")
		fmt.Println("LOCAL CLUSTER: " + localCluster)
		fmt.Println("REMOTE CLUSTER: " + remoteCluster)
		fmt.Println("ADAPTER: " + adapter)

		absChartPath, _ := filepath.Abs(chart)
		chartDirName := filepath.Base(absChartPath)

		//create temporary chart directory
		tempChartDir, err := ioutil.TempDir("", "")
		support.CheckError("", err, true)

		//check to see if valid chart directory passed in.
		//if not pull from repo
		if support.FileExists(chart + "/Chart.yaml") {
			_, stdErr, err := support.ExecuteSingleCommand([]string{"cp", "-a", absChartPath, tempChartDir})
			support.CheckError(stdErr, err, true)
		} else {
			_, stdErr, err := support.ExecuteSingleCommand([]string{HelmBin, "pull", chart, "--untar", "--untardir", tempChartDir})
			support.CheckError(stdErr, err, true)
		}

		//render chart and output to temporary directory
		_, stdErr, err = support.ExecuteSingleCommand(append(append([]string{HelmBin, "template"}, args[1:]...), "--output-dir", tempChartDir))
		support.CheckError(stdErr, err, true)

		processChart(tempChartDir+"/"+chartDirName, args)

		fmt.Println("\n--------------------------------------------------------------------------------------------------------------------------------")

		args[argPos] = tempChartDir + "/" + chartDirName
		stdOut, stdErr, err := support.ExecuteSingleCommand(append([]string{HelmBin}, args...))
		support.CheckError(stdErr, err, false)

		//delete temporary chart directory
		_, stdErr, err = support.ExecuteSingleCommand([]string{"rm", "-rf", tempChartDir})
		support.CheckError(stdErr, err, true)

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
		if !template.IsDir() && strings.HasSuffix(template.Name(), ".yaml") {

			manifest, err := ioutil.ReadFile(chartPath + "/templates/" + template.Name())
			support.CheckError("", err, true)

			var manifestYAML map[string]interface{}
			if err := yaml.Unmarshal([]byte(manifest), &manifestYAML); err != nil {
				continue
			}

			objType := manifestYAML["kind"].(string)
			objName := manifestYAML["metadata"].(map[string]interface{})["name"].(string)

			if _, ok := objTypeContainerPath[objType]; !ok {
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
				insight, approvalSetting, err := getInsight(remoteCluster, objNamespace, objType, objName, containerName)
				fmt.Print(strconv.Itoa(i) + "." + containerName + ": [" + approvalSetting + "] ")
				if err != nil {
					fmt.Println(err)
					fmt.Print("  Checking Cluster: ")
					insight, err = extractResourceSpecFromK8S(remoteCluster, objNamespace, objType, objName, containerName)
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

	jsonPath := objTypeContainerPath[objType]

	stdOut, stdErr, err := support.ExecuteSingleCommand([]string{KubectlBin, "get", objType, objName, "-o=jsonpath=" + jsonPath, "--cluster=" + cluster, "--namespace=" + objNamespace})
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

	return nil, errors.New("could not locate resource spec")

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
		kubeconfig, stdErr, err = support.ExecuteSingleCommand([]string{KubectlBin, "config", "view"})
	}
	support.CheckError(stdErr, err, true)

	var kubeconfigYAML map[string]interface{}
	err = yaml.Unmarshal([]byte(kubeconfig), &kubeconfigYAML)
	support.CheckError("", err, true)

	//determine current-context
	if kubecontext == "" {
		kubecontext = kubeconfigYAML["current-context"].(string)
	}

	//determine local cluster
	contextList := kubeconfigYAML["contexts"].([]interface{})
	for _, context := range contextList {
		if context.(map[string]interface{})["name"] == kubecontext {
			localCluster = context.(map[string]interface{})["context"].(map[string]interface{})["cluster"].(string)
		}
	}

	if val, ok := support.RetrieveSecrets("helm-optimize-plugin")["remoteCluster"]; ok {
		remoteCluster = val
	} else {

		support.LoadConfigMap()
		if support.Config != nil {
			if clusterName, ok := support.Config.Get("cluster_name"); ok {
				remoteCluster = clusterName
			} else if clusterName, ok := support.Config.Get("prometheus_address"); ok {
				remoteCluster = clusterName
			}
		} else {
			fmt.Println("could not resolve remote cluster -- please configure manually using 'helm optimize -c --cluster-mapping'")
			os.Exit(0)
		}

		support.StoreSecrets("helm-optimize-plugin", map[string]string{"remoteCluster": remoteCluster})

	}

}
