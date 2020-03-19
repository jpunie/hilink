package main

//go:generate go run gen.go

import (
	"encoding/json"
	// "flag"
	"reflect"
	"fmt"
	"log"
	"os"
	// "sort"
	// "strings"
	"time"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/jpunie/hilink"
)

const SMS_COMMAND_PREFIX_APN_SET = "apnSet,"
const SMS_COMMAND_PREFIX_APN_DEL = "apnDel"
const SMS_COMMAND_MODEM_INFO = "modemInfo"
// const SMS_COMMAND_REBOOT = "reboot"
// const SMS_COMMAND_RESET = "reset"
// const SMS_COMMAND_STATUS = "status"
// const SMS_COMMAND_INFO = "info"

type ProfileRequest struct {
	Name  string `json:"Name"`
	ApnName  string `json:"ApnName"`
	Username  string `json:"Username"`
	Password  string `json:"Password"`
	IsDefault  bool `json:"IsDefault"`
}

type ConnectionRequest struct {
	Roaming  string `json:"Roaming"`
	MaxIdleTime  string `json:"MaxIdleTime"`
	DataSwitch string `json:"DataSwitch"`
}

func getHilinkClient() (*hilink.Client, error) {
	// create client
	var opts = []hilink.Option{
		// hilink.Log(log.Printf, log.Printf),
	}
	client, err := hilink.NewClient(opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	return client, err;	
}

func getJsonEncoder(w http.ResponseWriter) (*json.Encoder) {
	var encoder = json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder
}

func rootLink(w http.ResponseWriter, r *http.Request) {
	getJsonEncoder(w).Encode(map[string]string{
		"/": "Hilink Proxy Root, list of available URIs",
		"/device-info": "General modem information",  
		"/connection-info": "General modem connection settings",  
		"/profile-info": "Connection profiles used by HiLink modem",
		"/profiles": "Connection profiles used by HiLink modem",
		"/sms": "SMS API for HiLink modem",
	})
}

func getDeviceInfo(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}
	deviceInfo, err := client.DeviceInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return 
	}
	getJsonEncoder(w).Encode(deviceInfo)
}

func getProfileInfo(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}
	profileInfo, err := client.ProfileInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return 
	}
	getJsonEncoder(w).Encode(profileInfo)
}

func getCurrentProfile(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}

	profileInfo, err := client.ProfileInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return 
	}
	
	profileIndex := profileInfo["CurrentProfile"].(string)
	if profileIndex == "0" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	var profiles = profileInfo["Profiles"].(map[string]interface{})["Profile"]
	getJsonEncoder(w).Encode(getProfileWithIndexFromProfiles(profiles, profileIndex))
}

func listProfiles(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}
	profileInfo, err := client.ProfileInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return 
	}
	getJsonEncoder(w).Encode(profileInfo["Profiles"].(map[string]interface{})["Profile"])
}

func getProfile(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}
	var profileIndex = mux.Vars(r)["index"]
	profileInfo, err := client.ProfileInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	var profiles = profileInfo["Profiles"].(map[string]interface{})["Profile"]
	if reflect.TypeOf(profiles).Kind() == reflect.Slice {
		for _, element := range profiles.([]interface {}) {
			if element.(map[string]interface {})["Index"] == profileIndex {
				getJsonEncoder(w).Encode(element)
				return 
			}
		}
	} 
	w.WriteHeader(http.StatusNotFound)
	getJsonEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Profile with index %s not found", profileIndex)})
}

func createNewProfileFromRequest(client *hilink.Client, newProfile ProfileRequest) (bool, error) {
	return client.ProfileAdd(
		newProfile.Name,
		newProfile.ApnName,
		newProfile.Username,
		newProfile.Password,
		newProfile.IsDefault,
	)
}

func createProfile(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}
	var newProfile ProfileRequest
	reqBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = json.Unmarshal(reqBody, &newProfile)
	if err != nil {
		http.Error(w, "Error while parsing request body", http.StatusBadRequest)
		return
	}

	flag, err := createNewProfileFromRequest(client, newProfile)

	if !flag {
		http.Error(w, "Call returned with failure", http.StatusInternalServerError)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	getJsonEncoder(w).Encode(getProfileWithName(client, newProfile.Name))
}

func getProfileWithName(client *hilink.Client, name string) (map[string]interface{}) {
	profileInfo, err := client.ProfileInfo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load profiles, error: %v\n", err)
		return nil
	}
	var profiles = profileInfo["Profiles"].(map[string]interface{})["Profile"]
	if reflect.TypeOf(profiles).Kind() == reflect.Slice {
		for _, element := range profiles.([]interface {}) {
			if element.(map[string]interface {})["Name"] == name {
				return element.(map[string]interface {})
			}
		}
	} 
	if reflect.TypeOf(profiles).Kind() == reflect.Map {
		if profiles.(map[string]interface {})["Name"] == name {
			return profiles.(map[string]interface {})
		}
	}
	return nil
}  

func getProfileWithIndex(client *hilink.Client, index string) (map[string]interface{}) {
	profileInfo, err := client.ProfileInfo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load profiles, error: %v\n", err)
		return nil
	}
	var profiles = profileInfo["Profiles"].(map[string]interface{})["Profile"]
	return getProfileWithIndexFromProfiles(profiles, index)
}

func getProfileWithIndexFromProfiles(profiles interface{}, index string) (map[string]interface{}) {
	if reflect.TypeOf(profiles).Kind() == reflect.Slice {
		for _, element := range profiles.([]interface {}) {
			if element.(map[string]interface {})["Index"] == index {
				return element.(map[string]interface {})
			}
		}
	} 
	if reflect.TypeOf(profiles).Kind() == reflect.Map {
		if profiles.(map[string]interface {})["Index"] == index {
			return profiles.(map[string]interface {})
		}
	}
	return nil
}  

func deleteProfile(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}
	flag, err := deleteProfileWithIndex(client, mux.Vars(r)["index"])
	if !flag {
		http.Error(w, "Call returned with failure", http.StatusInternalServerError)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func deleteProfileWithIndex(client *hilink.Client, profileIndex string) (bool, error) {
	profileInfo, err := client.ProfileInfo()
	if err != nil {
		return false, err
	}

	var currentProfile string = profileInfo["CurrentProfile"].(string)
	if currentProfile == profileIndex {
		currentProfile = "1"
	} 
	
	var profiles = profileInfo["Profiles"].(map[string]interface{})["Profile"]
	var profileCount int = 1;
	if reflect.TypeOf(profiles).Kind() == reflect.Slice {
		profileCount = len(profiles.([]interface{}))	
	} 	
	if profileCount == 1 {
		currentProfile = "0"
	}

	return client.ProfileDelete(
		profileIndex,
		currentProfile,
	)
}

func remapConnectionInfo(connectionInfo, dataswitch map[string]interface{}) (map[string]string) {
	return map[string]string{
		"Roaming": connectionInfo["RoamAutoConnectEnable"].(string),
		"MaxIdleTime": connectionInfo["MaxIdelTime"].(string),
		"DataSwitch": dataswitch["dataswitch"].(string),
	}
}

func getConnectionInfo(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}
	connectionInfo, err := client.ConnectionInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return 
	}	

	dataswitch, err := client.MobileDataSwitch()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return 
	} 	
	getJsonEncoder(w).Encode(remapConnectionInfo(connectionInfo, dataswitch))
}

func setConnectionInfo(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}

	var connectionRequest ConnectionRequest
	reqBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = json.Unmarshal(reqBody, &connectionRequest)
	if err != nil {
		http.Error(w, "Error while parsing request body", http.StatusBadRequest)
		return
	}
	
	flag, err := client.ConnectionProfile(
		connectionRequest.Roaming,
		connectionRequest.MaxIdleTime,
	)

	if !flag {
		http.Error(w, "Call returned with failure", http.StatusInternalServerError)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	flag, err = client.MobileDataSwitchState(connectionRequest.DataSwitch)

	if !flag {
		http.Error(w, "Call returned with failure", http.StatusInternalServerError)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	connectionInfo, err := client.ConnectionInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return 
	}
	dataswitch, err := client.MobileDataSwitch()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return 
	}
	getJsonEncoder(w).Encode(remapConnectionInfo(connectionInfo, dataswitch))
}

func connect(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}
	
	flag, err := client.Connect()
	if !flag {
		http.Error(w, "Call returned with failure", http.StatusInternalServerError)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	flag, err = client.MobileDataSwitchState("1")
	if !flag {
		http.Error(w, "Call returned with failure", http.StatusInternalServerError)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func disconnect(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return	
	}
	
	flag, err := client.Disconnect()
	if !flag {
		http.Error(w, "Call returned with failure", http.StatusInternalServerError)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	flag, err = client.MobileDataSwitchState("0")
	if !flag {
		http.Error(w, "Call returned with failure", http.StatusInternalServerError)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func checkForSms() {
	for true {
		time.Sleep(2 * time.Second)
		client, err := getHilinkClient()
		if err == nil {	
			l, err := client.SmsList(uint(hilink.SmsBoxTypeInbox), 1, 50, false, true, true)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
			}
			smsCount := len(l)
			messages := l["Messages"].(map[string]interface{})["Message"]
			if smsCount == 1 {
				handleSms(client, messages.(map[string]interface{}))	
			} else if smsCount > 0 {
				for _, element := range messages.([]interface{}) {
					handleSms(client, element.(map[string]interface{}))	
				}
			}	
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
}

func handleSms(client *hilink.Client, message map[string]interface{}) {
	// fmt.Println(message)
	// TODO parse date and remove SMS older than 2 days?
	messageContent := message["Content"].(string)
	if (strings.HasPrefix(messageContent, SMS_COMMAND_PREFIX_APN_SET)) {
		handleSetApnSms(client, message)	
		deleteSms(client, message)
	}
	if (strings.HasPrefix(messageContent, SMS_COMMAND_PREFIX_APN_DEL)) {
		handleDeleteApnSms(client, message)	
		deleteSms(client, message)
	}
	if (strings.HasPrefix(messageContent, SMS_COMMAND_MODEM_INFO)) {
		handleModemInfoSms(client, message)
		deleteSms(client, message)
	}
}

func handleSetApnSms(client *hilink.Client, message map[string]interface{}) {
	fmt.Println(message)
	messageContent := message["Content"].(string)
	phoneNumber := message["Phone"].(string)
	parts := strings.Split(messageContent, ",")
	partCount := len(parts)
	var profile ProfileRequest
	if (partCount == 2) {
		// TODO check if profile with apn-name exists
		profile = ProfileRequest{
			Name: parts[1],
			ApnName: parts[1],
			Username: "",
			Password: "",
			IsDefault: true,
		}
	} else if (partCount == 4) {
		profile = ProfileRequest{
			Name: parts[1],
			ApnName: parts[1],
			Username: parts[2],
			Password: parts[3],
			IsDefault: true,
		}
	}  else {
		sendSms(client, "APN config failed! Invalid content!", phoneNumber)
		return 
	}

	flag, err := createNewProfileFromRequest(client, profile)
	if err != nil {
		// TODO check if profile with apn-name exists and retry			 
		sendSms(client, fmt.Sprintf("APN %s config failed! %v", parts[1], err), phoneNumber)
		return
	} 

	if flag	{
		sendSms(client, fmt.Sprintf("APN %s set successfuly!", parts[1]), phoneNumber)
	} else {
		sendSms(client, fmt.Sprintf("APN %s config failed! No error available!", parts[1]), phoneNumber)
	}
}

func handleDeleteApnSms(client *hilink.Client, message map[string]interface{}) {
	fmt.Println(message)
	messageContent := message["Content"].(string)
	phoneNumber := message["Phone"].(string)
	parts := strings.Split(messageContent, ",")
	partCount := len(parts)
	if (partCount == 2) {
		profile := getProfileWithName(client, parts[1])
		if profile == nil {
			sendSms(client, fmt.Sprintf("APN %s delete failed! Not found!", parts[1]), phoneNumber)
			return
		}	
		flag, err := deleteProfileWithIndex(client, profile["Index"].(string))	

		if err != nil {
			// TODO check if profile with apn-name exists and retry			 
			sendSms(client, fmt.Sprintf("APN %s delete failed! %v", parts[1], err), phoneNumber)
			return
		} 
	
		if flag	{
			sendSms(client, fmt.Sprintf("APN %s delete successfuly!", parts[1]), phoneNumber)
		} else {
			sendSms(client, fmt.Sprintf("APN %s delete failed! No error available!", parts[1]), phoneNumber)
		}
	
	} else {
		sendSms(client, "APN delete failed! Invalid content!", phoneNumber)
		return
	}
}

func handleModemInfoSms(client *hilink.Client, message map[string]interface{}) {
	fmt.Println(message)
	phoneNumber := message["Phone"].(string)
	deviceInfo, err := client.DeviceInfo()
	if err != nil {
		sendSms(client, fmt.Sprintf("ModemInfo failed! %v", err), phoneNumber)
		return
	}	
	sendSms(client, strings.Join(filterDeviceInfo(deviceInfo), ","), phoneNumber)
}
  
func filterDeviceInfo(m map[string]interface{}) []string {
	keys := []string{
		"DeviceName", 
		"HardwareVersion",
		"Iccid",
		"Imei",
		"Imsi",
		"MacAddress1",
		"Msisdn",
		"WanIPAddress",
		"WanIPv6Address",
		"workmode",
	}
	return valuesForKeysInMap(m, keys)
}

func mapFilter(m map[string]interface{}, keys []string) map[string]interface{} {
	var result map[string]interface{}
	result = make(map[string]interface{})
	for _, k := range keys {
		result[k] = m[k]
	}
	return result
}

func valuesForKeysInMap(m map[string]interface{}, keys []string) []string {
	values := make([]string, len(keys))
	i := 0
	for _, k := range keys {
		values[i] = m[k].(string)
		i++
	}
	return values
}

func mapValues(m map[string]interface{}) []string {
	values := make([]string, len(m))
	i := 0
	for _, v := range m {
		values[i] = v.(string)
		i++
	}
	return values
}

func sendSms(client *hilink.Client, content, recipient string) {
	b, err := client.SmsSend(content, recipient)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	if !b {
		fmt.Fprintf(os.Stderr, "could not send message\n")
	}
}

func deleteSms(client *hilink.Client, message map[string]interface{}) {
	b, err := client.SmsDelete(message["Index"].(string))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	if !b {
		fmt.Fprintf(os.Stderr, "could not send message\n")
	}
}

func main() {
	// initEvents()
	go checkForSms()
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/", rootLink)
	router.HandleFunc("/device-info", getDeviceInfo).Methods("GET")
	router.HandleFunc("/profile-info", getProfileInfo).Methods("GET")
	router.HandleFunc("/current-profile", getCurrentProfile).Methods("GET")
	router.HandleFunc("/profiles", listProfiles).Methods("GET")
	router.HandleFunc("/profiles", createProfile).Methods("POST")
	router.HandleFunc("/profiles/{index}", getProfile).Methods("GET")
	router.HandleFunc("/profiles/{index}", deleteProfile).Methods("DELETE")
	router.HandleFunc("/connection-info", getConnectionInfo).Methods("GET")
	router.HandleFunc("/connection-info", setConnectionInfo).Methods("PUT")
	router.HandleFunc("/connect", connect).Methods("POST")
	router.HandleFunc("/disconnect", disconnect).Methods("POST")

	log.Fatal(http.ListenAndServe("127.0.0.1:1103", router))
}

