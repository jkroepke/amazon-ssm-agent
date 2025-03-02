// Copyright 2020 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may not
// use this file except in compliance with the License. A copy of the
// License is located at
//
// http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
// either express or implied. See the License for the specific language governing
// permissions and limitations under the License.

// Package main represents the entry point of the agent.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/aws/amazon-ssm-agent/agent/appconfig"
	logger "github.com/aws/amazon-ssm-agent/agent/log"
	"github.com/aws/amazon-ssm-agent/agent/proxyconfig"
	"github.com/aws/amazon-ssm-agent/core/app"
	"github.com/aws/amazon-ssm-agent/core/app/bootstrap"
	"github.com/aws/amazon-ssm-agent/core/app/runtimeconfiginit"
	"github.com/aws/amazon-ssm-agent/core/ipc/messagebus"
	"github.com/aws/amazon-ssm-agent/core/workerprovider/longrunningprovider/datastore/filesystem"
)

const (
	activationCodeFlag          = "code"
	activationIDFlag            = "id"
	regionFlag                  = "region"
	registerFlag                = "register"
	disableSimilarityCheckFlag  = "disableSimilarityCheck"
	versionFlag                 = "version"
	fingerprintFlag             = "fingerprint"
	similarityThresholdFlag     = "similarityThreshold"
	roleFlag                    = "role"
	tagsFlag                    = "tags"
	toolFlag                    = "tools"
	winOnFirstInstallChecksFlag = "winOnFirstInstallChecks"
	allowLinkDeletionsFlag      = "allowLinkDeletions"
)

var (
	activationCode, activationID, region, role, tagsJson string
	register, clear, force, fpFlag, tool                 bool
	agentVersionFlag                                     bool
	disableSimilarityCheck                               bool
	winOnFirstInstallChecks                              bool
	allowLinkDeletions                                   string
	similarityThreshold                                  int
	registrationFile                                     = filepath.Join(appconfig.DefaultDataStorePath, "registration")
)

func start(log logger.T) (app.CoreAgent, logger.T, error) {
	log.WriteEvent(logger.AgentTelemetryMessage, "", logger.AmazonAgentStartEvent)

	proxyConfig := proxyconfig.SetProxyConfig(log)
	log.Infof("Proxy environment variables:")
	for key, value := range proxyConfig {
		log.Infof(key + ": " + value)
	}

	bs := bootstrap.NewBootstrap(log, filesystem.NewFileSystem())
	context, err := bs.Init()
	if err != nil {
		return nil, log, err
	}

	// Initialize runtime configs
	rci := runtimeconfiginit.New(context.Log(), context.Identity())
	err = rci.Init()
	if err != nil {
		return nil, context.Log(), err
	}

	context = context.With("[amazon-ssm-agent]")
	message := messagebus.NewMessageBus(context)
	if err := message.Start(); err != nil {
		return nil, log, fmt.Errorf("failed to start message bus, %s", err)
	}

	ssmAgentCore := app.NewSSMCoreAgent(context, message)
	if err := ssmAgentCore.Start(); err != nil {
		return nil, log, fmt.Errorf("failed to start core agent app: %v", err)
	}

	return ssmAgentCore, context.Log(), nil
}

func blockUntilSignaled(log logger.T) {
	// Below channel will handle all machine initiated shutdown/reboot requests.

	// Set up channel on which to receive signal notifications.
	// We must use a buffered channel or risk missing the signal
	// if we're not ready to receive when the signal is sent.
	c := make(chan os.Signal, 1)

	// Listening for OS signals is a blocking call.
	// Only listen to signals that require us to exit.
	// Otherwise we will continue execution and exit the program.
	signal.Notify(c, os.Interrupt, os.Kill, syscall.SIGTERM)

	s := <-c
	log.Info("amazon-ssm-agent got signal:", s, " value:", s.Signal)
}

// Run as a single process. Used by Unix systems and when running agent from console.
func run(log logger.T) {
	defer func() {
		// recover in case the agent panics
		// this should handle some kind of seg fault errors.
		if msg := recover(); msg != nil {
			log.Errorf("core Agent crashed with message %v!", msg)
			log.Errorf("%s: %s", msg, debug.Stack())
		}
	}()

	// run ssm agent
	coreAgent, contextLog, err := start(log)
	if err != nil {
		contextLog.Errorf("error occurred when starting amazon-ssm-agent: %v", err)
		return
	}
	blockUntilSignaled(contextLog)
	coreAgent.Stop()
}
