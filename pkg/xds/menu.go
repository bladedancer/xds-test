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
	action       func(*Worker) error
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

func promptListString(label string, def []string) ([]string, error) {
	str, err := promptString(label+" (space separated)", strings.Join(def, " "))
	if err != nil {
		return nil, err
	}
	return strings.Fields(str), nil
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
		items := append([]string{"New Secret"}, secretNames...)
		action, _ = selectFromList("Select Secret", items, "")
	}

	var secret = ""
	if action == "New Secret" {
		// ADD Secret
		secret, err = addSecretInternal(worker)
		if err != nil {
			return "", err
		}
	} else {
		secret = action
	}

	return secret, nil
}

func selectFromList(label string, items []string, addLabel string) (string, error) {
	if addLabel != "" {
		selecter := promptui.SelectWithAdd{
			Label:    label,
			Items:    items,
			AddLabel: addLabel,
			Validate: validateRequired,
		}

		_, item, err := selecter.Run()
		return item, err
	} else {
		search := func(input string, index int) bool {
			return fuzzy.MatchFold(input, items[index])
		}
		selecter := promptui.Select{
			Label:    label,
			Items:    items,
			Searcher: search,
		}

		_, item, err := selecter.Run()
		return item, err
	}
}

func addListener(worker *Worker) error {
	var err error
	// Name
	name, err := promptString("Listener Name", "amplify")
	if err != nil {
		return err
	}

	// Port
	port, err := promptPort("Listener Port", 8080)
	if err != nil {
		return err
	}

	useTls, err := promptBool("Use TLS", false)
	if err != nil {
		return err
	}

	servername := ""
	secret := ""
	if useTls {
		servername, err = promptString("Server Name Match", "")
		if err != nil {
			return err
		}

		secret, err = promptSecretSelection(worker)
		if err != nil {
			return err
		}
	}

	routeConfigNames := worker.GetRouteConfigurationNames()
	routeConfigName, err := selectFromList("Select Route Configuration", routeConfigNames, "Route Configuration Name")
	if err != nil {
		return err
	}

	worker.AddListener(name, port, secret, []string{servername}, routeConfigName)

	return nil
}

func deleteListener(worker *Worker, listenerName string) error {
	worker.DeleteListener(listenerName)
	return nil
}

func addListenerFilterChain(worker *Worker) error {
	var err error
	listenerNames := worker.GetListenerNames()
	if len(listenerNames) == 0 {
		return errors.New("no listeners to update")
	}
	name, err := selectFromList("Select Listener", listenerNames, "")
	if err != nil {
		return err
	}

	useTls, err := promptBool("Use TLS", false)
	if err != nil {
		return err
	}

	var servernames []string = nil
	secret := ""
	if useTls {
		servernames, err = promptListString("Server Name Match", nil)
		if err != nil {
			return err
		}

		secret, err = promptSecretSelection(worker)
		if err != nil {
			return err
		}
	}

	routeConfigNames := worker.GetRouteConfigurationNames()
	routeConfigName, err := selectFromList("Route Configuration", routeConfigNames, "Route Configuration Name")
	if err != nil {
		return err
	}

	worker.AddListenerFilterChain(name, routeConfigName, secret, servernames)

	return nil
}

func updateListenerFilterChain(worker *Worker, listenerName string, routeConfigName string) {
	// TODO GET EXISTING VALUES
	useTls, err := promptBool("Use TLS", false)
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	var servernames []string = nil
	secret := ""
	if useTls {
		servernames, err = promptListString("Server Name Match", nil)
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

	worker.AddListenerFilterChain(listenerName, routeConfigName, secret, servernames)
}

func deleteListenerFilterChain(worker *Worker, listenerName string, routeConfigName string) {
	worker.DeleteListenerFilterChain(listenerName, routeConfigName)
}

func addCluster(worker *Worker) error {
	name, err := promptString("Cluster Name", fmt.Sprintf("cluster-%d", time.Now().Unix()))
	if err != nil {
		return err
	}

	host, err := promptString("Cluster Host", "localhost")
	if err != nil {
		return err
	}

	port, err := promptPort("Cluster Port", 80)
	if err != nil {
		return err
	}

	tls, err := promptBool("Use TLS", (port == 8443 || port == 443))
	if err != nil {
		return err
	}

	mtlsSecret := ""
	if tls {
		mtls, err := promptBool("Use mTLS", false)
		if err != nil {
			return err
		}

		if mtls {
			mtlsSecret, err = promptSecretSelection(worker)
			if err != nil {
				return err
			}
		}
	}
	worker.AddCluster(name, host, port, tls, mtlsSecret)
	return nil
}

func updateCluster(worker *Worker, name string) error {
	hostname, port, tls := worker.GetClusterDetails(name)
	hostname, err := promptString("Cluster Port", hostname)
	if err != nil {
		return err
	}

	port, err = promptPort("Cluster Port", port)
	if err != nil {
		return err
	}

	tls, err = promptBool("Use TLS", tls)
	if err != nil {
		return err
	}

	mtlsSecret := ""
	if tls {
		mtls, err := promptBool("Use mTLS", false)
		if err != nil {
			return err
		}

		if mtls {
			mtlsSecret, err = promptSecretSelection(worker)
			if err != nil {
				return err
			}
		}
	}

	worker.UpdateCluster(name, hostname, port, tls, mtlsSecret)
	return nil
}

func deleteCluster(worker *Worker, name string) error {
	worker.DeleteCluster(name)
	return nil
}

func addRouteConfiguration(worker *Worker) error {
	name, err := promptString("Route config name", fmt.Sprintf("rc-%d", time.Now().Unix()))
	if err != nil {
		return err
	}

	domains, err := promptListString("Domains", []string{"localhost"})
	if err != nil {
		return err
	}

	worker.AddRouteConfiguration(name, domains)
	return nil
}

func updateRouteConfiguration(worker *Worker, name string) error {
	domains := worker.GetRouteConfigurationDetails(name)
	domains, err := promptListString("Domains", domains)
	if err != nil {
		return err
	}

	worker.UpdateRouteConfiguration(name, domains)
	return nil
}

func deleteRouteConfiguration(worker *Worker, name string) error {
	worker.DeleteRouteConfiguration(name)
	return nil
}

func addRoute(worker *Worker, routeConfigName string) error {
	name, err := promptString("Route name", fmt.Sprintf("route-%d", time.Now().Unix()))
	if err != nil {
		return err
	}

	prefix, err := promptString("Path Prefix", "/")
	if err != nil {
		return err
	}

	clusterNames := worker.GetClusterNames()
	cluster, err := selectFromList("Select Cluster", clusterNames, "Cluster")
	if err != nil {
		return err
	}

	worker.AddRoute(routeConfigName, name, prefix, cluster)
	return nil
}

func updateRoute(worker *Worker, routeConfigName string, name string) error {
	prefix, _ := worker.GetRouteDetails(routeConfigName, name)
	prefix, err := promptString("Route Prefix", prefix)
	if err != nil {
		return err
	}

	clusterNames := worker.GetClusterNames()
	cluster, err := selectFromList("Select Cluster", clusterNames, "Cluster")
	if err != nil {
		return err
	}

	worker.UpdateRoute(routeConfigName, name, prefix, cluster)
	return nil
}

func deleteRoute(worker *Worker, routeConfigName string, name string) error {
	worker.DeleteRoute(routeConfigName, name)
	return nil
}

func addSecret(worker *Worker) error {
	_, err := addSecretInternal(worker)
	if err != nil {
		return err
	}
	return nil
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

func updateSecret(worker *Worker, name string) error {
	keyPath, certPath, password := worker.GetSecretDetails(name)
	keyPath, err := promptFile("Key Path", keyPath)
	if err != nil {
		return err
	}
	certPath, err = promptFile("Cert Path", certPath)
	if err != nil {
		return err
	}
	password, err = promptOptionalString("Password", password)
	if err != nil {
		return err
	}
	worker.UpdateSecret(name, keyPath, certPath, password)

	return nil
}

func deleteSecret(worker *Worker, name string) error {
	worker.DeleteSecret(name)
	return nil
}

func menuMain(worker *Worker) {
	cont := true
	menuItems := []*MenuItem{
		{
			label:        "Listener",
			confirmation: "Listener",
			action:       menuListener,
		},
		{
			label:        "Route Configuration",
			confirmation: "Route Configuration",
			action:       menuRouteConfiguration,
		},
		{
			label:        "Secret",
			confirmation: "Secret",
			action:       menuSecret,
		},
		{
			label:        "Cluster",
			confirmation: "Cluster",
			action:       menuCluster,
		},
		{
			label:        "Route",
			confirmation: "Route",
			action:       menuRoute,
		},
		{
			label:        "Deploy",
			confirmation: "Deploying",
			action:       func(w *Worker) error { w.MarkDirty(); return nil },
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
		err = selection.action(worker)

		if err != nil {
			log.Error(err)
		}
	}
}

type newItemFunc func(*Worker) error
type editItemFunc func(*Worker, string) error
type deleteItemFunc func(*Worker, string) error

func menuAddOrEditOrDelete(worker *Worker, label string, createLabel string, items []string, newItem newItemFunc, editItem editItemFunc, deleteItem deleteItemFunc) error {
	list := append([]string{createLabel}, items...)
	if len(list) > 1 {
		list = append(list, "DELETE")
	}
	opt, err := selectFromList(label, list, "")
	if err != nil {
		return err
	}

	if opt == createLabel {
		err = newItem(worker)
	} else if opt == "DELETE" {
		toDelete, err := selectFromList("Delete", items, "")
		if err != nil {
			return err
		}
		err = deleteItem(worker, toDelete)
	} else {
		err = editItem(worker, opt)
	}

	return err
}

func menuListener(worker *Worker) error {
	return menuAddOrEditOrDelete(
		worker,
		"Add or Edit Listener",
		"New Listener",
		worker.GetListenerNames(),
		addListener,
		menuListenerSingle,
		deleteListener,
	)
}

func menuListenerSingle(worker *Worker, listenerName string) error {

	return menuAddOrEditOrDelete(
		worker,
		"Add or Edit Listener Filter Chain",
		"New Filter Chain",
		worker.GetListenerNames(),
		addListenerFilterChain,
		func(w *Worker, rcName string) error { updateListenerFilterChain(w, listenerName, rcName); return nil },
		func(w *Worker, rcName string) error { deleteListenerFilterChain(w, listenerName, rcName); return nil },
	)
}

func menuSecret(worker *Worker) error {
	return menuAddOrEditOrDelete(
		worker,
		"Add or Edit Secret",
		"New Secret",
		worker.GetSecretNames(),
		addSecret,
		updateSecret,
		deleteSecret,
	)
}

func menuCluster(worker *Worker) error {
	return menuAddOrEditOrDelete(
		worker,
		"Add or Edit Cluster",
		"New Cluster",
		worker.GetClusterNames(),
		addCluster,
		updateCluster,
		deleteCluster,
	)
}

func menuRoute(worker *Worker) error {
	routeConfigNames := worker.GetRouteConfigurationNames()
	routeConfigName, err := selectFromList("Route Configuration", routeConfigNames, "Route Configuration Name")
	if err != nil {
		return err
	}

	return menuAddOrEditOrDelete(
		worker,
		"Add or Edit Route",
		"New Route",
		worker.GetRouteNames(routeConfigName),
		func(w *Worker) error { addRoute(w, routeConfigName); return nil },
		func(w *Worker, name string) error { updateRoute(w, routeConfigName, name); return nil },
		func(w *Worker, name string) error { deleteRoute(w, routeConfigName, name); return nil },
	)
}

func menuRouteConfiguration(worker *Worker) error {
	return menuAddOrEditOrDelete(
		worker,
		"Add or Edit Route Configuration",
		"New Route Configuration",
		worker.GetRouteConfigurationNames(),
		addRouteConfiguration,
		updateRouteConfiguration,
		deleteRouteConfiguration,
	)
}
