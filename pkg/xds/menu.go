package xds

import (
	"errors"
	"fmt"
	"time"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/manifoldco/promptui"
)

type MenuItem struct {
	label        string
	confirmation string
	action       func(*Worker)
}

func updateListener(worker *Worker) {
	worker.UpdateListener()
}

func addCluster(worker *Worker) {
	validate := func(input string) error {
		if len(input) < 3 {
			return errors.New("Input must have more than 3 characters")
		}
		return nil
	}

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

	worker.AddCluster(name, host)
}

func updateCluster(worker *Worker) {
	validate := func(input string) error {
		if len(input) < 3 {
			return errors.New("Input must have more than 3 characters")
		}
		return nil
	}

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

	hostname := worker.GetClusterHostname(name)
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

	worker.UpdateCluster(name, host)
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
