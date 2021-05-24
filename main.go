package main

import (
	"embed"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"

	"github.com/jessevdk/go-flags"
	log "github.com/sirupsen/logrus"
)

var version = "devel" // set by the build process

// Embedded filesystem

//go:embed templates/*
var embedFs embed.FS

// printPeerInfo prints a peer's configuration to the log
func printPeerInfo(peerName string, peerData *peer) {
	// Fields to exclude from print output
	excludedFields := []string{"PrefixSet4", "PrefixSet6", "Name", "SessionGlobal", "PreImport", "PreExport", "PreImportFinal", "PreExportFinal", "QueryTime"}
	s := reflect.ValueOf(peerData).Elem()
	typeOf := s.Type()
	for i := 0; i < s.NumField(); i++ {
		attrName := typeOf.Field(i).Name
		if !(contains(excludedFields, attrName)) {
			log.Infof("[%s] attribute %s = %v\n", peerName, attrName, s.Field(i).Interface())
		}
	}
}

func main() {
	// Parse cli flags
	_, err := flags.ParseArgs(&cliFlags, os.Args)
	if err != nil {
		if !strings.Contains(err.Error(), "Usage") {
			log.Fatal(err)
		}
		os.Exit(1)
	}

	// Enable debug logging in development releases
	if //noinspection GoBoolExpressions
	version == "devel" || cliFlags.Verbose {
		log.SetLevel(log.DebugLevel)
	}

	if cliFlags.ShowVersion {
		log.Printf("wireframe version %s (https://github.com/natesales/wireframe)\n", version)
		os.Exit(0)
	}

	log.Infof("Starting  %s", version)

	// Load templates from embedded filesystem
	err = loadTemplates(embedFs)
	if err != nil {
		log.Fatal(err)
	}

	log.Debug("Finished loading templates")

	// Load the config file from config file
	log.Debugf("Loading config from %s", cliFlags.ConfigFile)
	configFile, err := ioutil.ReadFile(cliFlags.ConfigFile)
	if err != nil {
		log.Fatal("reading config file: " + err.Error())
	}

	globalConfig, err := loadConfig(configFile)
	if err != nil {
		log.Fatal(err)
	}

	if !cliFlags.DryRun {
		// Create the global output file
		log.Debug("Creating global config")
		globalFile, err := os.Create(path.Join(globalConfig.BirdDirectory, "bird.conf"))
		if err != nil {
			log.Fatalf("Create global BIRD output file: %v", err)
		}
		log.Debug("Finished creating global config file")

		// Render the global template and write to disk
		log.Debug("Writing global config file")
		err = globalTemplate.ExecuteTemplate(globalFile, "global.tmpl", globalConfig)
		if err != nil {
			log.Fatalf("Execute global template: %v", err)
		}
		log.Debug("Finished writing global config file")

		// Remove old peer-specific configs
		files, err := filepath.Glob(path.Join(globalConfig.BirdSocket, "AS*.conf"))
		if err != nil {
			log.Fatal(err)
		}
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				log.Fatalf("Removing old config files: %v", err)
			}
		}
	} else {
		log.Info("Dry run is enabled, skipped writing global config and removing old peer configs")
	}

	// Iterate over peers
	for peerName, peerData := range globalConfig.Peers {
		// Set sanitized peer name
		if unicode.IsDigit(rune(peerName[0])) {
			// Add peer prefix if the first character of peerName is a number
			peerData.ProtocolName = "PEER_" + sanitize(peerName)
		} else {
			peerData.ProtocolName = sanitize(peerName)
		}

		log.Infof("Checking config for %s AS%d", peerName, peerData.Asn)

		//	if !peerData.NoPeeringDB {
		//		// Only query PeeringDB and IRRDB for peers and downstreams, TODO: This should validate upstreams too
		//		peerData.QueryTime = time.Now().Format(time.RFC1123)
		//		peeringDbData := getPeeringDbData(peerData.Asn)
		//
		//		if peerData.ImportLimit4 == 0 {
		//			peerData.ImportLimit4 = peeringDbData.MaxPfx4
		//			log.Infof("[%s] has no IPv4 import limit configured. Setting to %d from PeeringDB", peerName, peeringDbData.MaxPfx4)
		//		}
		//
		//		if peerData.ImportLimit6 == 0 {
		//			peerData.ImportLimit6 = peeringDbData.MaxPfx6
		//			log.Infof("[%s] has no IPv6 import limit configured. Setting to %d from PeeringDB", peerName, peeringDbData.MaxPfx6)
		//		}
		//
		//		// Only set AS-SET from PeeringDB if it isn't configure manually
		//		if peerData.AsSet == "" {
		//			// If the as-set has a space in it, split and pick the first element
		//			if strings.Contains(peeringDbData.AsSet, " ") {
		//				peeringDbData.AsSet = strings.Split(peeringDbData.AsSet, " ")[0]
		//				log.Warnf("[%s] has a space in their PeeringDB as-set field. Selecting first element %s", peerName, peeringDbData.AsSet)
		//			}
		//
		//			// Trim IRRDB prefix
		//			if strings.Contains(peeringDbData.AsSet, "::") {
		//				peerData.AsSet = strings.Split(peeringDbData.AsSet, "::")[1]
		//				log.Warnf("[%s] has a IRRDB prefix in their PeeringDB as-set field. Using %s", peerName, peerData.AsSet)
		//			} else {
		//				peerData.AsSet = peeringDbData.AsSet
		//			}
		//
		//			if peeringDbData.AsSet == "" {
		//				log.Warnf("[%s] has no as-set in PeeringDB, falling back to their ASN (%d)", peerName, peerData.Asn)
		//				peerData.AsSet = fmt.Sprintf("AS%d", peerData.Asn)
		//			} else {
		//				log.Infof("[%s] has no manual AS-SET defined. Setting to %s from PeeringDB\n", peerName, peeringDbData.AsSet)
		//			}
		//		} else {
		//			log.Infof("[%s] has manual AS-SET: %s", peerName, peerData.AsSet)
		//		}
		//
		//		//peerData.PrefixSet4 = getPrefixFilter(peerData.AsSet, 4, globalConfig.IrrDb)
		//		//peerData.PrefixSet6 = getPrefixFilter(peerData.AsSet, 6, globalConfig.IrrDb)
		//
		//		// Update the "latest operation" timestamp
		//		//peerData.QueryTime = time.Now().Format(time.RFC1123)
		//	}
		//
		//	// If as-set is empty and the peer type requires it
		//	if peerData.AsSet == "" && (peerData.Type == "peer" || peerData.Type == "downstream") {
		//		log.Fatalf("[%s] has no AS-SET defined and filtering profile requires it.", peerName)
		//	}
		//
		//	// Print peer info
		//	printPeerInfo(peerName, peerData)
		//
		//	if !cliFlags.DryRun {
		//		// Create the peer specific file
		//		peerSpecificFile, err := os.Create(path.Join(globalConfig.BirdDirectory, "AS"+strconv.Itoa(int(peerData.Asn))+"_"+normalize(peerName)+".conf"))
		//		if err != nil {
		//			log.Fatalf("Create peer specific output file: %v", err)
		//		}
		//
		//		// Render the template and write to disk
		//		log.Infof("[%s] Writing config", peerName)
		//		err = peerTemplate.ExecuteTemplate(peerSpecificFile, "peer.tmpl", &Wrapper{Peer: *peerData, Config: *globalConfig})
		//		if err != nil {
		//			log.Fatalf("Execute template: %v", err)
		//		}
		//
		//		log.Infof("[%s] Wrote config", peerName)
		//	} else {
		//		log.Infof("Dry run is enabled, skipped writing peer config(s)")
		//	}
		//}
		//
		//if !cliFlags.DryRun {
		//	// Write VRRP config
		//	writeVrrpConfig(globalConfig)
		//
		//	if globalConfig.BirdSocket != "" {
		//		writeUiFile(globalConfig)
		//	} else {
		//		log.Infof("--ui-file is not defined, not creating a UI file")
		//	}
		//
		//	if !cliFlags.NoConfigure {
		//		log.Infoln("Reconfiguring BIRD")
		//		if err = runBirdCommand("configure", globalConfig.BirdSocket); err != nil {
		//			log.Fatal(err)
		//		}
		//	} else {
		//		log.Infoln("Option --no-configure is set, NOT reconfiguring bird")
		//	}
		//
		//	// Configure interfaces
		//	configureInterfaces(globalConfig)
		//}
	}
}
