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
	systemsEP   = "/CIRBA/api/v2/systems"
)

//Initialize will initilize the densify secrets k8s object, if it doesn't exist in the current-context.
func Initialize() error {

	//check stored secret
	storedSecrets := support.RetrieveSecrets("optimize-adapter-config")
	if storedSecrets != nil && storedSecrets["adapter"] == "densify" {
		densifyURL = storedSecrets["densifyURL"]
		densifyUser = storedSecrets["densifyUser"]
		densifyPass = storedSecrets["densifyPass"]
		if err := validateSecrets(); err == nil {
			return nil
		}
	}

	//check data forwarder config map
	if support.Config != nil {

		var host, protocol, port string
		var ok bool
		if host, ok = support.Config.Get("host"); !ok {
			return errors.New("could not extract host from densify data forwarder configMap")
		}
		if protocol, ok = support.Config.Get("protocol"); !ok {
			return errors.New("could not extract protocol from densify data forwarder configMap")
		}
		if port, ok = support.Config.Get("port"); !ok {
			return errors.New("could not extract port from densify data forwarder configMap")
		}

		densifyURL = protocol + "://" + host + ":" + port
		fmt.Println("DensifyURL: " + densifyURL)

	}

	//if we can't resolve creds, then fetch from user
	if densifyURL == "" {
		fmt.Print("Densify URL: ")
		fmt.Scanln(&densifyURL)
		densifyURL = strings.TrimSuffix(densifyURL, "/")
	}

	fmt.Print("Densify Username: ")
	fmt.Scanln(&densifyUser)

	fmt.Print("Densify Password: ")
	pass, _ := terminal.ReadPassword(0)
	densifyPass = string(pass)
	fmt.Println("")

	if err := validateSecrets(); err != nil {
		return err
	}

	storeSecrets()

	return nil

}

//GetInsight gets an insight from densify based on the keys cluster, namespace, objType, objName and containerName
func GetInsight(cluster string, namespace string, objType string, objName string, containerName string) (map[string]map[string]string, string, error) {

	err := loadInsightCache(cluster)
	if err != nil {
		return nil, "", errors.New("unable to locate resource spec")
	}

	for _, insight := range insightCache[cluster] {

		if insight.Cluster == cluster && insight.Namespace == namespace && insight.ControllerType == objType &&
			insight.PodService == objName && insight.Container == containerName {

			var insightObj = map[string]map[string]string{}
			insightObj["limits"] = map[string]string{}
			insightObj["requests"] = map[string]string{}

			approvalSetting, _ := getAttribute(insight.EntityID, "attr_ApprovalSetting")

			if approvalSetting == "Approve Specific Change" || approvalSetting == "Approve Any Change" {

				insightObj["limits"]["cpu"] = strconv.Itoa(insight.RecommendedCPULimit) + "m"
				insightObj["limits"]["memory"] = strconv.Itoa(insight.RecommendedMemLimit) + "Mi"
				insightObj["requests"]["cpu"] = strconv.Itoa(insight.RecommendedCPURequest) + "m"
				insightObj["requests"]["memory"] = strconv.Itoa(insight.RecommendedMemRequest) + "Mi"

			} else if insight.CurrentCPULimit > 0 && insight.CurrentMemLimit > 0 && insight.CurrentCPURequest > 0 && insight.CurrentMemRequest > 0 {

				insightObj["limits"]["cpu"] = strconv.Itoa(insight.CurrentCPULimit) + "m"
				insightObj["limits"]["memory"] = strconv.Itoa(insight.CurrentMemLimit) + "Mi"
				insightObj["requests"]["cpu"] = strconv.Itoa(insight.CurrentCPURequest) + "m"
				insightObj["requests"]["memory"] = strconv.Itoa(insight.CurrentMemRequest) + "Mi"

			} else {

				return nil, "", errors.New("invalid resource specs received from repository")

			}

			return insightObj, approvalSetting, nil

		}

	}

	return nil, "", errors.New("unable to locate resource spec")

}

//UpdateApprovalSetting this will update the approval status for a specific recommendation
func UpdateApprovalSetting(approved bool, cluster string, namespace string, objType string, objName string, containerName string) error {

	err := loadInsightCache(cluster)
	if err != nil {
		return errors.New("unable to update approval setting")
	}

	var entityID string
	for _, insight := range insightCache[cluster] {

		if insight.Cluster == cluster && insight.Namespace == namespace && insight.ControllerType == objType && insight.PodService == objName && insight.Container == containerName {

			entityID = insight.EntityID

		}

	}

	if approved == true {
		_, err = support.HTTPRequest("PUT", densifyURL+systemsEP+"/"+entityID+"/attributes", densifyUser+":"+densifyPass, []byte("[{\"name\": \"Approval Setting\", \"value\": \"Approve Specific Change\"}]"))
	} else {
		_, err = support.HTTPRequest("PUT", densifyURL+systemsEP+"/"+entityID+"/attributes", densifyUser+":"+densifyPass, []byte("[{\"name\": \"Approval Setting\", \"value\": \"Not Approved\"}]"))
	}

	return err

}

//GetApprovalSetting this will update the approval status for a specific recommendation
func GetApprovalSetting(cluster string, namespace string, objType string, objName string, containerName string) (string, error) {

	err := loadInsightCache(cluster)
	if err != nil {
		return "", errors.New("unable to read approval setting")
	}

	for _, insight := range insightCache[cluster] {

		if insight.Cluster == cluster && insight.Namespace == namespace && insight.ControllerType == objType && insight.PodService == objName && insight.Container == containerName {

			approvalSetting, err := getAttribute(insight.EntityID, "attr_ApprovalSetting")
			if err != nil {
				return "", err
			}
			return approvalSetting, nil

		}

	}

	return "", errors.New("unable to read approval setting")

}

func loadInsightCache(cluster string) error {

	if _, ok := insightCache[cluster]; !ok {
		resp, err := support.HTTPRequest("GET", densifyURL+analysisEP, densifyUser+":"+densifyPass, nil)
		if err != nil {
			return err
		}
		var analyses []interface{}
		found := false
		json.Unmarshal([]byte(resp), &analyses)
		for _, analysis := range analyses {
			if analysis.(map[string]interface{})["analysisName"].(string) == cluster {
				resp, err = support.HTTPRequest("GET", densifyURL+analysisEP+"/"+analysis.(map[string]interface{})["analysisId"].(string)+"/results", densifyUser+":"+densifyPass, nil)
				if err != nil {
					return err
				}

				var insights []Insight
				json.Unmarshal([]byte(resp), &insights)
				insightCache[cluster] = insights
				found = true
				break
			}
		}
		if found == false {
			return errors.New("unable to locate resource spec")
		}
	}

	return nil

}

func getAttribute(entityID string, attrID string) (string, error) {

	resp, err := support.HTTPRequest("GET", densifyURL+systemsEP+"/"+entityID, densifyUser+":"+densifyPass, nil)
	if err != nil {
		return "", errors.New("error locating attribute[" + attrID + "]")
	}

	var respMap map[string]interface{}
	json.Unmarshal([]byte(resp), &respMap)

	for _, val := range respMap["attributes"].([]interface{}) {
		if val.(map[string]interface{})["id"] == attrID {
			return val.(map[string]interface{})["value"].(string), nil
		}
	}
	return "Not Approved", nil

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

func storeSecrets() {

	storeSecrets := make(map[string]string)
	storeSecrets["adapter"] = "densify"
	storeSecrets["densifyURL"] = densifyURL
	storeSecrets["densifyUser"] = densifyUser
	storeSecrets["densifyPass"] = densifyPass
	support.StoreSecrets("optimize-adapter-config", storeSecrets)

}
