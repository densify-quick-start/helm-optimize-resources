package densify

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/densify-quick-start/helm-optimize-resources/support"
	"golang.org/x/crypto/ssh/terminal"
)

var insightCache = make(map[string][]Insight)

//Insight this struct holds a recommendation
type Insight struct {
	Container       string  `json:"container"`
	RecommFirstSeen int64   `json:"recommFirstSeen"`
	Cluster         string  `json:"cluster"`
	HostName        string  `json:"hostName,omitempty"`
	PredictedUptime float64 `json:"predictedUptime,omitempty"`
	ControllerType  string  `json:"controllerType"`
	DisplayName     string  `json:"displayName"`
	RecommLastSeen  int64   `json:"recommLastSeen"`
	EntityID        string  `json:"entityId"`
	PodService      string  `json:"podService"`
	AuditInfo       struct {
		DataCollection struct {
			DateFirstAudited int64 `json:"dateFirstAudited"`
			AuditCount       int   `json:"auditCount"`
			DateLastAudited  int64 `json:"dateLastAudited"`
		} `json:"dataCollection"`
		WorkloadDataLast30 struct {
			TotalDays int   `json:"totalDays"`
			SeenDays  int   `json:"seenDays"`
			FirstDate int64 `json:"firstDate"`
			LastDate  int64 `json:"lastDate"`
		} `json:"workloadDataLast30"`
	} `json:"auditInfo,omitempty"`
	RecommendedCPULimit   int    `json:"recommendedCpuLimit,omitempty"`
	RecommendedMemRequest int    `json:"recommendedMemRequest,omitempty"`
	CurrentCount          int    `json:"currentCount"`
	RecommSeenCount       int    `json:"recommSeenCount"`
	Namespace             string `json:"namespace"`
	RecommendedMemLimit   int    `json:"recommendedMemLimit,omitempty"`
	RecommendationType    string `json:"recommendationType"`
	RecommendedCPURequest int    `json:"recommendedCpuRequest,omitempty"`
	CurrentMemLimit       int    `json:"currentMemLimit,omitempty"`
	CurrentMemRequest     int    `json:"currentMemRequest,omitempty"`
	CurrentCPULimit       int    `json:"currentCpuLimit,omitempty"`
	CurrentCPURequest     int    `json:"currentCpuRequest,omitempty"`
}

var (
	densifyURL  string
	densifyUser string
	densifyPass string
	analysisEP  = "/CIRBA/api/v2/analysis/containers/kubernetes"
	authorizeEP = "/CIRBA/api/v2/authorize"
)

//Initialize will initilize the densify secrets k8s object, if it doesn't exist in the current-context.
func Initialize() error {

	_, err := support.RetrieveStoredSecret("optimize-plugin-secrets", "adapter")
	if err != nil {

		fmt.Print("Densify URL: ")
		fmt.Scanln(&densifyURL)
		densifyURL = strings.TrimSuffix(densifyURL, "/")

		fmt.Print("Densify Username: ")
		fmt.Scanln(&densifyUser)

		fmt.Print("Densify Password: ")
		pass, _ := terminal.ReadPassword(0)
		densifyPass = string(pass)
		fmt.Println("")

		_, stdErr, err := support.ExecuteSingleCommand([]string{"kubectl", "create", "secret", "generic", "optimize-plugin-secrets", "--from-literal=adapter=densify", "--from-literal=densifyURL=" + densifyURL, "--from-literal=densifyUser=" + densifyUser, "--from-literal=densifyPass=" + densifyPass})
		if err != nil {
			return errors.New(stdErr)
		}

	} else {

		densifyURL, err = support.RetrieveStoredSecret("optimize-plugin-secrets", "densifyURL")
		if err != nil {
			return err
		}

		densifyUser, err = support.RetrieveStoredSecret("optimize-plugin-secrets", "densifyUser")
		if err != nil {
			return err
		}
		densifyPass, err = support.RetrieveStoredSecret("optimize-plugin-secrets", "densifyPass")
		if err != nil {
			return err
		}

	}

	if err = validateSecrets(); err != nil {
		return err
	}

	return nil

}

//GetInsight gets an insight from densify based on the keys cluster, namespace, objType, objName and containerName
func GetInsight(cluster string, namespace string, objType string, objName string, containerName string) (map[string]map[string]string, error) {

	if _, ok := insightCache[cluster]; !ok {
		resp, err := support.HTTPRequest("GET", densifyURL+analysisEP, densifyUser+":"+densifyPass, nil)
		if err != nil {
			return nil, err
		}
		var analyses []interface{}
		found := false
		json.Unmarshal([]byte(resp), &analyses)
		for _, analysis := range analyses {
			if analysis.(map[string]interface{})["analysisName"].(string) == cluster {
				resp, err = support.HTTPRequest("GET", densifyURL+analysisEP+"/"+analysis.(map[string]interface{})["analysisId"].(string)+"/results", densifyUser+":"+densifyPass, nil)
				if err != nil {
					return nil, err
				}

				var insights []Insight
				json.Unmarshal([]byte(resp), &insights)
				insightCache[cluster] = insights
				found = true
				break
			}
		}
		if found == false {
			return nil, errors.New("unable to locate insight in densify")
		}
	}

	for _, insight := range insightCache[cluster] {

		if insight.Cluster == cluster && insight.Namespace == namespace && insight.ControllerType == objType &&
			insight.PodService == objName && insight.Container == containerName && insight.RecommendedCPULimit > 0 &&
			insight.RecommendedCPURequest > 0 && insight.RecommendedMemLimit > 0 && insight.RecommendedMemRequest > 0 {

			var insightObj = map[string]map[string]string{}
			insightObj["limits"] = map[string]string{}
			insightObj["requests"] = map[string]string{}

			insightObj["limits"]["cpu"] = strconv.Itoa(insight.RecommendedCPULimit) + "m"
			insightObj["limits"]["memory"] = strconv.Itoa(insight.RecommendedMemLimit) + "Mi"
			insightObj["requests"]["cpu"] = strconv.Itoa(insight.RecommendedCPURequest) + "m"
			insightObj["requests"]["memory"] = strconv.Itoa(insight.RecommendedMemRequest) + "Mi"

			return insightObj, nil
		}

	}

	return nil, errors.New("unable to locate insight in densify")

}

func validateSecrets() error {

	jsonReq, err := json.Marshal(map[string]string{
		"userName": densifyUser,
		"pwd":      densifyPass,
	})
	if err != nil {
		return err
	}

	_, err = support.HTTPRequest("POST", densifyURL+authorizeEP, densifyUser+":"+densifyPass, jsonReq)
	if err != nil {
		return err
	}

	return nil

}
