package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"

	"eino-quickstart/internal/indexversion"
)

func main() {
	statePath := flag.String("state", "logs/index-versions.json", "version state JSON path")
	command := flag.String("command", "status", "create|progress|complete|fail|switch|rollback|status")
	id := flag.String("id", "", "index version id")
	embedding := flag.String("embedding", "", "embedding model version")
	dimensions := flag.Int("dimensions", 0, "embedding dimensions")
	mapping := flag.String("mapping", "", "mapping fingerprint")
	expected := flag.Int("expected", 0, "expected indexed chunks")
	indexed := flag.Int("indexed", 0, "indexed chunks")
	reason := flag.String("reason", "", "failure reason")
	flag.Parse()

	manager, err := indexversion.New(*statePath)
	if err != nil {
		log.Fatal(err)
	}
	switch *command {
	case "create":
		err = manager.Create(indexversion.Version{ID: *id, Embedding: *embedding, Dimensions: *dimensions, Mapping: *mapping, Expected: *expected})
	case "progress":
		err = manager.Progress(*id, *indexed)
	case "complete":
		err = manager.Complete(*id)
	case "fail":
		err = manager.Fail(*id, *reason)
	case "switch":
		err = manager.Switch(*id)
	case "rollback":
		err = manager.Rollback(*id)
	case "status":
		active, versions, stateErr := manager.State()
		if stateErr == nil {
			data, _ := json.MarshalIndent(struct {
				Active   string                 `json:"active"`
				Versions []indexversion.Version `json:"versions"`
			}{active, versions}, "", "  ")
			fmt.Println(string(data))
		}
		err = stateErr
	default:
		err = fmt.Errorf("unknown command %q", *command)
	}
	if err != nil {
		log.Fatal(err)
	}
	if *command != "status" {
		fmt.Printf("%s %s\n", *command, *id)
	}
}
