package xds

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/manifoldco/promptui"
)

type MenuItem struct {
	label        string
	confirmation string
	action       func(*Worker)
}

var validate = func(input string) error {
	if len(input) < 3 {
		return errors.New("input must have more than 3 characters")
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

func updateListener(worker *Worker) {
	worker.UpdateListener()
}

func addCluster(worker *Worker) {
	prompt := promptui.Prompt{
		Label:    "Cluster name",
		Validate: validate,
		Default:  fmt.Sprintf("cluster-%d", time.Now().Unix()),
	}

	name, err := prompt.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	prompt = promptui.Prompt{
		Label:    "Cluster Host",
		Validate: validate,
		Default:  "localhost",
	}

	host, err := prompt.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	prompt = promptui.Prompt{
		Label:    "Cluster Port",
		Validate: validatePort,
		Default:  "80",
	}

	portStr, err := prompt.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	port, _ := strconv.Atoi(portStr)
	worker.AddCluster(name, host, uint32(port))
}

func updateCluster(worker *Worker) {
	clusterNames := worker.GetClusterNames()

	if len(clusterNames) == 0 {
		log.Info("No clusters to update")
		return
	}
	search := func(input string, index int) bool {
		return fuzzy.MatchFold(input, clusterNames[index])
	}

	selecter := promptui.Select{
		Label:    "Select Cluster",
		Items:    clusterNames,
		Searcher: search,
	}

	_, name, err := selecter.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	hostname, port := worker.GetClusterDetails(name)
	prompt := promptui.Prompt{
		Label:    "Cluster Host",
		Validate: validate,
		Default:  hostname,
	}

	host, err := prompt.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	prompt = promptui.Prompt{
		Label:    "Cluster Port",
		Validate: validatePort,
		Default:  strconv.Itoa(int(port)),
	}

	portStr, err := prompt.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	portInt, _ := strconv.Atoi(portStr)
	worker.UpdateCluster(name, host, uint32(portInt))
}

func addRoute(worker *Worker) {
	validate := func(input string) error {
		if len(input) < 3 {
			return errors.New("input must have more than 3 characters")
		}
		return nil
	}

	prompt := promptui.Prompt{
		Label:    "Route name",
		Validate: validate,
		Default:  fmt.Sprintf("route-%d", time.Now().Unix()),
	}

	name, err := prompt.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	prompt = promptui.Prompt{
		Label:   "Path Prefix",
		Default: "/",
	}

	prefix, err := prompt.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	clusterNames := worker.GetClusterNames()

	selecter := promptui.SelectWithAdd{
		Label:    "Select Cluster",
		Items:    clusterNames,
		AddLabel: "Other",
	}

	_, cluster, err := selecter.Run()

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
		log.Info("No routes to update")
		return
	}
	search := func(input string, index int) bool {
		return fuzzy.MatchFold(input, routeNames[index])
	}

	selecter := promptui.Select{
		Label:    "Select Route",
		Items:    routeNames,
		Searcher: search,
	}

	_, name, err := selecter.Run()
	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	prefix, _ := worker.GetRouteDetails(name)
	prompt := promptui.Prompt{
		Label:   "Route Prefix",
		Default: prefix,
	}

	prefix, err = prompt.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	clusterNames := worker.GetClusterNames()
	clusterSelecter := promptui.SelectWithAdd{
		Label:    "Select Cluster",
		Items:    clusterNames,
		AddLabel: "Other",
	}

	_, cluster, err := clusterSelecter.Run()

	if err != nil {
		log.Debug(err)
		log.Info("Cancelled")
		return
	}

	worker.AddRoute(name, prefix, cluster)
}

func menu(worker *Worker) {
	cont := true
	menuItems := []*MenuItem{
		{
			label:        "Update Listener",
			confirmation: "Updating Listener",
			action:       updateListener,
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
