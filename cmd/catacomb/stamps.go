package main

import (
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/stepkey"
)

func currentStamps() model.Stamps {
	return model.Stamps{CatacombVersion: Version, StepKeyScheme: stepkey.Scheme}
}
