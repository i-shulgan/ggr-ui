// +build watch

package main

import (
	"log"
	"time"

	"github.com/aandryashin/reloader"
)

func init() {
	startWatcher = watchQuota
}

func watchQuota() {
	err := reloader.Watch(quotaDir, func() {
		if err := configure(); err != nil {
			log.Printf("[-] [INIT] [Failed to reload quota files: %v]", err)
		}
	}, 5*time.Second)
	if err != nil {
		log.Fatalf("[-] [INIT] [Failed to init watching quota for changes: %v]", err)
	}
	log.Printf("[-] [INIT] [Watching quota directory %s for changes]", quotaDir)
}
