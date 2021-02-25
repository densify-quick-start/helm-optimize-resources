package ssm

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"../support"
)

var (
	keyPrefix string
	profile   string
	region    string
)

var supportedRegions = []string{"us-east-2", "us-east-1", "us-west-1", "us-west-2", "af-south-1", "ap-east-1", "ap-south-1", "ap-northeast-3", "ap-northeast-2", "ap-southeast-1", "ap-southeast-2", "ap-northeast-1", "ca-central-1", "cn-north-1", "cn-northwest-1", "eu-central-1", "eu-west-1", "eu-west-2", "eu-south-1", "eu-west-3", "eu-north-1", "me-south-1", "sa-east-1", "us-gov-east-1", "us-gov-west-1"}

//Initialize will ready the adapter to serve insight extraction from AWS parameter store.
func Initialize(create bool) error {

	//Check dependancies
	stdOut, stdErr, err := support.ExecuteSingleCommand([]string{"whereis", "aws"})
	support.CheckError(stdErr, err, true)
	if !strings.Contains(strings.Split(stdOut, ":")[1], "aws") {
		fmt.Println("aws-cli is not available.  please install before trying again.")
		return errors.New("")
	}

	//if creating new credentials
	if create {

		support.RemoveSecret("optimize-plugin-secrets")

		for {
			fmt.Print("What is your preferred parameter key prefix [no prefix]: ")
			fmt.Scanln(&keyPrefix)
			if keyPrefix != "" {
				if res1, _ := regexp.MatchString("^/{0,1}(aws|ssm)", keyPrefix); res1 {
					fmt.Println("Parameter name: can't be prefixed with \"aws\" or \"ssm\" (case-insensitive).")
					continue
				}

				if res1, _ := regexp.MatchString("^[a-zA-Z0-9_.-]*$", keyPrefix); !res1 {
					fmt.Println("Only a mix of letters, numbers and the following 3 symbols .-_ are allowed.")
					continue
				}
			}
			break
		}

		for {
			fmt.Print("What is your preferred AWS profile [default]: ")
			fmt.Scanln(&profile)
			if profile == "" {
				profile = "default"
			}
			_, stdErr, err = support.ExecuteSingleCommand([]string{"aws", "sts", "get-caller-identity", "--profile", profile})
			if found := support.CheckError(stdErr, err, false); !found {
				break
			}
		}

		for {
			fmt.Print("What is your preferred AWS region [us-east-1]: ")
			fmt.Scanln(&region)
			if region == "" {
				region = "us-east-1"
			}
			if _, ok := support.InSlice(supportedRegions, region); !ok {
				fmt.Println("Invalid entry.  Check for valid regions here https://aws.amazon.com/about-aws/global-infrastructure/regions_az/.")
				continue
			}
			break
		}

		_, stdErr, err := support.ExecuteSingleCommand([]string{"kubectl", "create", "secret", "generic", "optimize-plugin-secrets", "--from-literal=adapter=ssm", "--from-literal=profile=" + profile, "--from-literal=prefix=" + keyPrefix, "--from-literal=region=" + region})
		support.CheckError(stdErr, err, true)

	} else {

		region, err = support.RetrieveStoredSecret("optimize-plugin-secrets", "region")
		support.CheckError("", err, true)

		keyPrefix, err = support.RetrieveStoredSecret("optimize-plugin-secrets", "prefix")
		support.CheckError("", err, true)

		profile, err = support.RetrieveStoredSecret("optimize-plugin-secrets", "profile")
		support.CheckError("", err, true)

	}

	return nil

}

//GetInsight gets an insight from densify based on the keys cluster, namespace, objType, objName and containerName
func GetInsight(cluster string, namespace string, objType string, objName string, containerName string) (string, error) {

	var ssmKey string
	if keyPrefix == "" {
		ssmKey = "/" + cluster + "/" + namespace + "/" + objType + "/" + objName + "/" + containerName + "/resourceSpec"
	} else {
		ssmKey = "/" + keyPrefix + "/" + cluster + "/" + namespace + "/" + objType + "/" + objName + "/" + containerName + "/resourceSpec"
	}

	insight, stdErr, err := support.ExecuteSingleCommand([]string{"aws", "ssm", "get-parameter", "--with-decryption", "--name", ssmKey, "--profile", profile, "--region", region, "--query", "Parameter.Value"})
	if err != nil {
		fmt.Print(strings.ReplaceAll(stdErr, "\n", ""))
		return "", err
	}

	insight, err = strconv.Unquote(insight)
	support.CheckError("", err, true)

	var parsedInsight map[string]interface{}
	json.Unmarshal([]byte(insight), &parsedInsight)

	jsonInsight, err := json.Marshal(parsedInsight)
	support.CheckError("", err, true)

	return string(jsonInsight), nil

}
