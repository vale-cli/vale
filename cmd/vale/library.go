package main

import "encoding/json"

var library = "https://raw.githubusercontent.com/vale-cli/packages/master/library.json"

func getLibrary(_ string) ([]Style, error) {
	styles := []Style{}

	resp, err := fetchJSON(library)
	if err != nil {
		return styles, err
	} else if err = json.Unmarshal(resp, &styles); err != nil {
		return styles, err
	}

	return styles, err
}

// inLibrary returns the URL the library lists for name, or "" when it
// lists none. An error means the library itself couldn't be read.
func inLibrary(name, path string) (string, error) {
	lookup, err := getLibrary(path)
	if err != nil {
		return "", err
	}

	for _, entry := range lookup {
		if name == entry.Name {
			return entry.URL, nil
		}
	}

	return "", nil
}
