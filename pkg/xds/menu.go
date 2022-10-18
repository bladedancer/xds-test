package xds

import (
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/manifoldco/promptui"
)

type MenuItem struct {
	label        string
	confirmation string
	action       func()
}

func menu(worker *Worker) {
	cont := true
	menuItems := []*MenuItem{
		{
			label:        "Update Listener",
			confirmation: "Updating Listener",
			action:       worker.UpdateListener,
		},
		{
			label:        "Quit",
			confirmation: "Quiting",
			action:       func() { cont = false },
		},
	}

	menuLabels := []string{}
	for _, menuItem := range menuItems {
		menuLabels = append(menuLabels, menuItem.label)
	}

	search := func(input string, index int) bool {
		log.Info(input)
		log.Info(index)
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
		selection.action()
	}
}
