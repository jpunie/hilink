package main

//go:generate go run gen.go

import (
	"encoding/json"
	// "flag"
	"fmt"
	"log"
	"os"
	"reflect"

	// "sort"
	// "strings"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/jpunie/hilink"
)

const SMS_COMMAND_PREFIX_APN_SET = "apnSet,"
const SMS_COMMAND_PREFIX_APN_DEL = "apnDel"
const SMS_COMMAND_MODEM_INFO = "modemInfo"
const SMS_COMMAND_SMS_CLEAR = "smsClear"

// const SMS_COMMAND_REBOOT = "reboot"
// const SMS_COMMAND_RESET = "reset"
// const SMS_COMMAND_STATUS = "status"
// const SMS_COMMAND_INFO = "info"

const SMS_CHECK_DELAY = 5
const NETWORK_CHECK_DELAY = 30

type ProfileRequest struct {
	Name      string `json:"Name"`
	ApnName   string `json:"ApnName"`
	Username  string `json:"Username"`
	Password  string `json:"Password"`
	IsDefault bool   `json:"IsDefault"`
}

type ConnectionRequest struct {
	Roaming     string `json:"Roaming"`
	MaxIdleTime string `json:"MaxIdleTime"`
	DataSwitch  string `json:"DataSwitch"`
}

type SmsRequest struct {
	To      string `json:"To"`
	Message string `json:"Message"`
}

var hlc *hilink.Client

func getHilinkClient() (*hilink.Client, error) {
	if hlc != nil {
		return hlc, nil
	}
	// create client
	var opts = []hilink.Option{
		// hilink.Log(log.Printf, log.Printf),
	}
	var err error
	hlc, err = hilink.NewClient(opts...)
	if err != nil {
		hlc = nil
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	return hlc, err
}

func getJsonEncoder(w http.ResponseWriter) *json.Encoder {
	var encoder = json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder
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
	profilesWrapper := profileInfo["Profiles"]
	w.WriteHeader(http.StatusOK)
	if reflect.TypeOf(profilesWrapper).Kind() == reflect.String {
		getJsonEncoder(w).Encode([]int{})
		return
	} else {
		profiles := profilesWrapper.(map[string]interface{})["Profile"]
		if reflect.TypeOf(profiles).Kind() == reflect.Map {
			s := make([]map[string]interface{}, 1, 1)
			s[0] = profiles.(map[string]interface{})
			getJsonEncoder(w).Encode(s)
		} else {
			getJsonEncoder(w).Encode(profiles)
		}
	}
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
		for _, element := range profiles.([]interface{}) {
			if element.(map[string]interface{})["Index"] == profileIndex {
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

func getProfileWithName(client *hilink.Client, name string) map[string]interface{} {
	profileInfo, err := client.ProfileInfo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load profiles, error: %v\n", err)
		return nil
	}
	var profiles = profileInfo["Profiles"].(map[string]interface{})["Profile"]
	if reflect.TypeOf(profiles).Kind() == reflect.Slice {
		for _, element := range profiles.([]interface{}) {
			if element.(map[string]interface{})["Name"] == name {
				return element.(map[string]interface{})
			}
		}
	}
	if reflect.TypeOf(profiles).Kind() == reflect.Map {
		if profiles.(map[string]interface{})["Name"] == name {
			return profiles.(map[string]interface{})
		}
	}
	return nil
}

func getProfileWithIndex(client *hilink.Client, index string) map[string]interface{} {
	profileInfo, err := client.ProfileInfo()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load profiles, error: %v\n", err)
		return nil
	}
	var profiles = profileInfo["Profiles"].(map[string]interface{})["Profile"]
	return getProfileWithIndexFromProfiles(profiles, index)
}

func getProfileWithIndexFromProfiles(profiles interface{}, index string) map[string]interface{} {
	if reflect.TypeOf(profiles).Kind() == reflect.Slice {
		for _, element := range profiles.([]interface{}) {
			if element.(map[string]interface{})["Index"] == index {
				return element.(map[string]interface{})
			}
		}
	}
	if reflect.TypeOf(profiles).Kind() == reflect.Map {
		if profiles.(map[string]interface{})["Index"] == index {
			return profiles.(map[string]interface{})
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
	var profileCount int = 1
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

func remapConnectionInfo(connectionInfo, dataswitch, statusInfo map[string]interface{}) map[string]string {
	return map[string]string{
		"Roaming":            connectionInfo["RoamAutoConnectEnable"].(string),
		"MaxIdleTime":        connectionInfo["MaxIdelTime"].(string),
		"DataSwitch":         dataswitch["dataswitch"].(string),
		"ConnectionStatus":   statusInfo["ConnectionStatus"].(string),
		"CurrentNetworkType": statusInfo["CurrentNetworkType"].(string),
		"RoamingStatus":      statusInfo["RoamingStatus"].(string),
		"ServiceStatus":      statusInfo["ServiceStatus"].(string),
		"SimStatus":          statusInfo["SimStatus"].(string),
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

	statusInfo, err := client.StatusInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	getJsonEncoder(w).Encode(remapConnectionInfo(connectionInfo, dataswitch, statusInfo))
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
	// TODO make separate URI for connection-state
	statusInfo, err := client.StatusInfo()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	getJsonEncoder(w).Encode(remapConnectionInfo(connectionInfo, dataswitch, statusInfo))
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

func listSmsbox(w http.ResponseWriter, r *http.Request, boxType hilink.SmsBoxType) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	l, err := client.SmsList(uint(boxType), 1, 50, false, true, true)
	if err != nil {
		http.Error(w, "Call return with failure", http.StatusInternalServerError)
		return
	}
	smsCount, err := strconv.Atoi(l["Count"].(string))
	w.WriteHeader(http.StatusOK)
	if smsCount == 0 {
		getJsonEncoder(w).Encode([]int{})
		return
	}
	messages := l["Messages"].(map[string]interface{})["Message"]
	if reflect.TypeOf(messages).Kind() == reflect.Map {
		s := make([]map[string]interface{}, 1, 1)
		s[0] = messages.(map[string]interface{})
		getJsonEncoder(w).Encode(s)
	} else {
		getJsonEncoder(w).Encode(messages)
	}
}

func listSmsInbox(w http.ResponseWriter, r *http.Request) {
	listSmsbox(w, r, hilink.SmsBoxTypeInbox)
}

func deleteSmsApi(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var smsIndex = mux.Vars(r)["index"]
	_, err = client.SmsDelete(smsIndex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func listSmsOutbox(w http.ResponseWriter, r *http.Request) {
	listSmsbox(w, r, hilink.SmsBoxTypeOutbox)
}

func sendNewSms(w http.ResponseWriter, r *http.Request) {
	client, err := getHilinkClient()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var newSms SmsRequest
	reqBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = json.Unmarshal(reqBody, &newSms)
	if err != nil {
		http.Error(w, "Error while parsing request body", http.StatusBadRequest)
		return
	}

	flag, err := client.SmsSend(newSms.Message, newSms.To)

	if !flag {
		http.Error(w, "Call returned with failure", http.StatusInternalServerError)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func checkForSms() {
	for true {
		time.Sleep(SMS_CHECK_DELAY * time.Second)
		client, err := getHilinkClient()
		if err == nil {
			l, err := client.SmsList(uint(hilink.SmsBoxTypeInbox), 1, 50, false, true, true)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			smsCount, err := strconv.Atoi(l["Count"].(string))
			if smsCount > 0 {
				messages := l["Messages"].(map[string]interface{})["Message"]
				if reflect.TypeOf(messages).Kind() == reflect.Map {
					handleSms(client, messages.(map[string]interface{}))
				} else if reflect.TypeOf(messages).Kind() == reflect.Slice {
					for _, element := range messages.([]interface{}) {
						handleSms(client, element.(map[string]interface{}))
					}
				}
			}
			clearSmsbox(client, hilink.SmsBoxTypeOutbox)
			clearSmsbox(client, hilink.SmsBoxTypeDraft)
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
}

func handleSms(client *hilink.Client, message map[string]interface{}) {
	// fmt.Println(message)
	// TODO parse date and remove SMS older than 2 days?
	messageContent := message["Content"].(string)
	if strings.HasPrefix(messageContent, SMS_COMMAND_PREFIX_APN_SET) {
		handleSetApnSms(client, message)
		deleteSms(client, message)
	}
	if strings.HasPrefix(messageContent, SMS_COMMAND_PREFIX_APN_DEL) {
		handleDeleteApnSms(client, message)
		deleteSms(client, message)
	}
	if strings.HasPrefix(messageContent, SMS_COMMAND_MODEM_INFO) {
		handleModemInfoSms(client, message)
		deleteSms(client, message)
	}
	if strings.HasPrefix(messageContent, SMS_COMMAND_SMS_CLEAR) {
		handleSmsClearSms(client, message)
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
	if partCount == 2 {
		// TODO check if profile with apn-name exists
		profile = ProfileRequest{
			Name:      parts[1],
			ApnName:   parts[1],
			Username:  "",
			Password:  "",
			IsDefault: true,
		}
	} else if partCount == 4 {
		profile = ProfileRequest{
			Name:      parts[1],
			ApnName:   parts[1],
			Username:  parts[2],
			Password:  parts[3],
			IsDefault: true,
		}
	} else {
		sendSms(client, "APN config failed! Invalid content!", phoneNumber)
		return
	}

	flag, err := createNewProfileFromRequest(client, profile)
	if err != nil {
		// TODO check if profile with apn-name exists and retry
		sendSms(client, fmt.Sprintf("APN %s config failed! %v", parts[1], err), phoneNumber)
		return
	}

	if flag {
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
	if partCount == 2 {
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

		if flag {
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

func handleSmsClearSms(client *hilink.Client, message map[string]interface{}) {
	fmt.Println(message)
	phoneNumber := message["Phone"].(string)
	countOutbox, err1 := clearSmsbox(client, hilink.SmsBoxTypeOutbox)
	if err1 != nil {
		sendSms(client, fmt.Sprintf("Clear SMS outbox failed! %v", err1), phoneNumber)
		return
	}
	countInbox, err2 := clearSmsbox(client, hilink.SmsBoxTypeInbox)
	if err2 != nil {
		sendSms(client, fmt.Sprintf("Clear SMS inbox failed! %v", err2), phoneNumber)
		return
	}
	sendSms(client, fmt.Sprintf("Clear SMS boxes in: %d out: %d! ", countInbox, countOutbox), phoneNumber)
	clearSmsbox(client, hilink.SmsBoxTypeOutbox)
}

func clearSmsbox(client *hilink.Client, boxType hilink.SmsBoxType) (int, error) {
	l, err := client.SmsList(uint(boxType), 1, 50, false, true, false)
	if err != nil {
		return 0, err
	}
	smsCount, err := strconv.Atoi(l["Count"].(string))
	if smsCount > 0 {
		messages := l["Messages"].(map[string]interface{})["Message"]
		if smsCount == 1 {
			deleteSms(client, messages.(map[string]interface{}))
		} else if smsCount > 0 {
			for _, element := range messages.([]interface{}) {
				deleteSms(client, element.(map[string]interface{}))
			}
		}
	}
	return smsCount, nil
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

func sendSms(client *hilink.Client, content string, recipient string) {
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

func networkNotReachable() bool {
	_, err := http.Get("http://ifconfig.co")
	if err != nil {
		return true
	}
	return false
}

func checkInitializedAndConnected() {
	for true {
		time.Sleep(NETWORK_CHECK_DELAY * time.Second)
		client, err := getHilinkClient()
		if err == nil {
			deviceInfo, err := client.DeviceInfo()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				continue
			}
			ipAddress := deviceInfo["WanIPAddress"].(string)
			if ipAddress == "" && networkNotReachable() {
				// statusInfo, err := client.StatusInfo()
				// if err != nil {
				// 	fmt.Fprintf(os.Stderr, "error: %v\n", err)
				// 	continue
				// }
				changed, err := checkAndInitProfile(client, deviceInfo)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					continue
				}
				if changed {
					client.ConnectionProfile("1", "3600")
					client.MobileDataSwitchState("1")
					client.Connect()
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
}

func checkAndInitProfile(client *hilink.Client, deviceInfo map[string]interface{}) (bool, error) {
	imsi := deviceInfo["Imsi"].(string)
	imsiPrefix := imsi[0:5]
	var newProfile ProfileRequest
	switch imsiPrefix {
	case "20408":
		newProfile = ProfileRequest{
			Name:      "simpoint.m2m",
			ApnName:   "simpoint.m2m",
			Username:  "",
			Password:  "",
			IsDefault: true,
		}
	default:
		newProfile = ProfileRequest{
			Name:      "",
			ApnName:   "",
			Username:  "",
			Password:  "",
			IsDefault: true,
		}
	}
	if newProfile.Name != "" {
		flag, err := createNewProfileFromRequest(client, newProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "APN %s config failed! %v", newProfile.ApnName, err)
			return false, err
		}
		return flag, nil
	}
	return false, nil
}

func rootLink(w http.ResponseWriter, r *http.Request) {
	getJsonEncoder(w).Encode(map[string]string{
		"/":                 "Hilink Proxy Root, list of available URIs",
		"/device-info":      "General modem information",
		"/connect":          "Connect to mobile network",
		"/disconnect":       "Disconnect to mobile network",
		"/connection-info":  "General modem connection settings",
		"/current-profile":  "Connection profiles used by HiLink modem",
		"/profile-info":     "Connection profiles used by HiLink modem",
		"/profiles":         "Connection profiles available by HiLink modem",
		"/profiles/{index}": "Connection profile, remove with method DELETE",
		"/sms/inbox":        "List SMS from inbox",
		"/sms/outbox":       "List SMS from outbox, send SMS using method POST",
		"/sms/{index}":      "Delete SMS using method DELETE",
	})
}

func main() {
	// initEvents()
	go checkForSms()
	go checkInitializedAndConnected()

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
	router.HandleFunc("/sms/inbox", listSmsInbox).Methods("GET")
	router.HandleFunc("/sms/outbox", listSmsOutbox).Methods("GET")
	router.HandleFunc("/sms/outbox", sendNewSms).Methods("POST")
	router.HandleFunc("/sms/{index}", deleteSmsApi).Methods("DELETE")

	log.Fatal(http.ListenAndServe("127.0.0.1:1103", router))
}
