package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func saveField(data map[string]any) {
	content, _ := json.Marshal(data)
	err := os.WriteFile("form.json", content, 0644)
	if err != nil {
		panic(err)
	}
	fmt.Println("[*]Save the successful personal data into form.json")
}

func loadField() map[string]any {
	data, err := os.ReadFile("form.json")
	if err != nil {
		fmt.Println("[*]No form.json,it will save in project if you sign in successfully")
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		panic(err)
	}
	fmt.Println("[*]Load the saved data from form.json")
	return m

}
