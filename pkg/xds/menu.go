package xds

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/manifoldco/promptui"
)

type MenuItem struct {
	label        string
	confirmation string
	action       func(*Worker)
}

var validateRequired = func(input string) error {
	if len(input) == 0 {
		return errors.New("input required")
	}
	return nil
}

var validatePort = func(input string) error {
	i, err := strconv.Atoi(input)
	if err != nil {
		return errors.New("input must be a number")
	}

	if i < 1 || i > 65535 {
		return errors.New("input must be between 1 and 65535")
	}
	return nil
}

var validateFile = func(input string) error {
	if _, err := os.Stat(input); errors.Is(err, os.ErrNotExist) {
		return errors.New("path not found")
	}
	return nil
}

func promptOptionalString(label string, def string) (string, error) {
	prompt := promptui.Prompt{
		Label:   label,
		Default: def,
	}
	str, err := prompt.Run()
	if err != nil {
		return "", err
	}
	return str, nil
}

func promptString(label string, def string) (string, error) {
	prompt := promptui.Prompt{
		Label:    label,
		Validate: validateRequired,
		Default:  def,
	}
	str, err := prompt.Run()
	if err != nil {
		return "", err
	}
	return str, nil
}

func promptFile(label string, def string) (string, error) {
	prompt := promptui.Prompt{
		Label:    label,
		Validate: validateFile,
		Default:  def,
	}
	str, err := prompt.Run()
	if err != nil {
		return "", err
	}
	return str, nil
}

func promptPort(label string, def uint32) (uint32, error) {
	prompt := promptui.Prompt{
		Label:    label,
		Validate: validatePort,
		Default:  strconv.Itoa(int(def)),
	}
	portStr, err := prompt.Run()
	if err != nil {
		return 0, err
	}
	port, _ := strconv.Atoi(portStr)
	return uint32(port), nil
}

func promptBool(label string, def bool) (bool, error) {
	defStr := "N"
	if def {
		defStr = "Y"
	}
	prompt := promptui.Prompt{
		Label:     label,
		IsConfirm: true,
		Default:   defStr,
	}
	boolStr, _ := prompt.Run()
	return strings.ToLower(boolStr) == "y", nil
}

func promptSecretSelection(worker *Worker) (string, error) {
	var err error
	action := ""
	secretNames := worker.GetSecretNames()

	if len(secretNames) == 0 {
		action = "New Secret"
	} else {
		prompt := promptui.Select{
			Label: "Select Action",
			Items: []string{"New Secret", "Existing Secret"},
			Size:  2,
		}
		_, action, err = prompt.Run()
		if err != nil {
			return "", err
		}
	}

	var secret = ""
	if action == "New Secret" {
		// ADD Secret
		secret, err = addSecretInternal(worker)
		if err != nil {
			return "", err
		}
	} else {
		search := func(input string, index int) bool {
			return fuzzy.MatchFold(input, secretNames[index])
		}

		selecter := promptui.Select{
			Label:    "Select Secret",
			Items:    secretNames,
			Searcher: search,
		}

		_, secret, err = selecter.Run()
		if err != nil {
			return "", err
		}
	}

	return secret, nil
}

func selectFromList(label string, items []string, addLabel bool) (string, error) {
	if addLabel {
		selecter := promptui.SelectWithAdd{
			Label:    label,
			Items:    items,
			AddLabel: "Other",
		}

		_, cluster, err := selecter.Run()
		return cluster, err
	} else {
		search := func(input string, index int) bool {
			return fuzzy.MatchFold(input, items[index])
		}
		selecter := promptui.Select{
			Label:    label,
			Items:    items,
			Searcher: search,
		}

		_, cluster, err := selecter.Run()
		return cluster, err
	}
}

func addListener(worker *Worker) {
	var err error
	// Name
	name, err := promptString("Listener Name", "amplify")
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	// Port
	port, err := promptPort("Listener Port", 8080)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	useTls, err := promptBool("Use TLS", false)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	servername := ""
	secret := ""
	if useTls {
		servername, err = promptString("Server Name Match", "")
		if err != nil {
			log.Debug(err)
			log.Info("Cancelled")
			return
		}

		secret, err = promptSecretSelection(worker)
		if err != nil {
			log.Debug(err)
			log.Info("Cancelled")
			return
		}
	}

	worker.AddListener(name, port, secret, []string{servername})
}

func addListenerFilterChain(worker *Worker) {
	var err error
	listenerNames := worker.GetListenerNames()
	if len(listenerNames) == 0 {
		log.Info("No listeners to update")
		return
	}
	name, err := selectFromList("Select Listener", listenerNames, false)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	useTls, err := promptBool("Use TLS", false)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	servername := ""
	secret := ""
	if useTls {
		servername, err = promptString("Server Name Match", "")
		if err != nil {
			log.Debug(err)
			log.Info("Cancelled")
			return
		}

		secret, err = promptSecretSelection(worker)
	}

	worker.AddListenerFilterChain(name, secret, []string{servername})
}

func addCluster(worker *Worker) {
	name, err := promptString("Cluster Name", fmt.Sprintf("cluster-%d", time.Now().Unix()))
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	host, err := promptString("Cluster Host", "localhost")
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	port, err := promptPort("Cluster Port", 80)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	worker.AddCluster(name, host, port)
}

func updateCluster(worker *Worker) {
	clusterNames := worker.GetClusterNames()
	if len(clusterNames) == 0 {
		log.Info("No clusters to update")
		return
	}
	name, err := selectFromList("Select Cluster", clusterNames, false)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	hostname, port := worker.GetClusterDetails(name)
	hostname, err = promptString("Cluster Port", hostname)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	port, err = promptPort("Cluster Port", port)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	worker.UpdateCluster(name, hostname, port)
}

func addRoute(worker *Worker) {
	name, err := promptString("Route name", fmt.Sprintf("route-%d", time.Now().Unix()))
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	prefix, err := promptString("Path Prefix", "/")
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	clusterNames := worker.GetClusterNames()
	cluster, err := selectFromList("Select Cluster", clusterNames, true)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	worker.AddRoute(name, prefix, cluster)
}

func updateRoute(worker *Worker) {
	routeNames := worker.GetRouteNames()
	if len(routeNames) == 0 {
		log.Info("No Routes")
		return
	}
	name, err := selectFromList("Select Route", routeNames, false)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	prefix, _ := worker.GetRouteDetails(name)
	prefix, err = promptString("Route Prefix", prefix)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	clusterNames := worker.GetClusterNames()
	cluster, err := selectFromList("Select Cluster", clusterNames, true)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	worker.UpdateRoute(name, prefix, cluster)
}

func addSecret(worker *Worker) {
	_, err := addSecretInternal(worker)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}
}

func addSecretInternal(worker *Worker) (string, error) {
	secret, err := promptString("Secret Name", fmt.Sprintf("secret-%d", time.Now().Unix()))
	if err != nil {
		return "", err
	}

	cwd, _ := os.Getwd()
	keyPath, err := promptFile("Key Path", fmt.Sprintf("%s/private-key.pem", cwd))
	if err != nil {
		return "", err
	}
	certPath, err := promptFile("Cert Path", fmt.Sprintf("%s/certificate.pem", cwd))
	if err != nil {
		return "", err
	}
	password, err := promptOptionalString("Password", "")
	if err != nil {
		return "", err
	}
	worker.AddSecret(secret, keyPath, certPath, password)
	return secret, nil
}

func updateSecret(worker *Worker) {
	secrets := worker.GetSecretNames()
	if len(secrets) == 0 {
		log.Info("No Secrets")
		return
	}
	name, err := selectFromList("Select secret", secrets, false)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	keyPath, certPath, password := worker.GetSecretDetails(name)
	keyPath, err = promptFile("Key Path", keyPath)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}
	certPath, err = promptFile("Cert Path", certPath)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}
	password, err = promptOptionalString("Password", password)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}
	worker.UpdateSecret(name, keyPath, certPath, password)
}

func menu(worker *Worker) {
	cont := true
	menuItems := []*MenuItem{
		{
			label:        "Add Listener",
			confirmation: "Adding Listener",
			action:       addListener,
		},
		{
			label:        "Add Filter Chain",
			confirmation: "Adding Filter Chin",
			action:       addListenerFilterChain,
		},
		{
			label:        "Add Secret",
			confirmation: "Adding Secret",
			action:       addSecret,
		},
		{
			label:        "Update Secret",
			confirmation: "Updating Secret",
			action:       updateSecret,
		},
		{
			label:        "Add Cluster",
			confirmation: "Adding Cluster",
			action:       addCluster,
		},
		{
			label:        "Update Cluster",
			confirmation: "Updating Cluster",
			action:       updateCluster,
		},
		{
			label:        "Add Route",
			confirmation: "Adding Route",
			action:       addRoute,
		},
		{
			label:        "Update Route",
			confirmation: "Updating Route",
			action:       updateRoute,
		},
		{
			label:        "Quit",
			confirmation: "Quiting",
			action:       func(w *Worker) { cont = false },
		},
	}

	menuLabels := []string{}
	for _, menuItem := range menuItems {
		menuLabels = append(menuLabels, menuItem.label)
	}

	search := func(input string, index int) bool {
		return fuzzy.MatchFold(input, menuItems[index].label)
	}

	getMenuItem := func(label string) *MenuItem {
		for _, menuItem := range menuItems {
			if menuItem.label == label {
				return menuItem
			}
		}
		return nil
	}

	for cont {
		prompt := promptui.Select{
			Label:    "Select Action",
			Items:    menuLabels,
			Size:     len(menuLabels),
			Searcher: search,
		}

		_, result, err := prompt.Run()

		if err != nil {
			log.Debugf("Prompt failed %v", err)
			return
		}

		selection := getMenuItem(result)
		log.Info(selection.confirmation)
		selection.action(worker)
	}
}
