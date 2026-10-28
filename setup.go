package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"Jarvis/internal/config"
	"Jarvis/internal/ollama"
	"Jarvis/internal/setup"
	"Jarvis/internal/voice"
)

const (
	eventSetupProgress  = "setup:progress"
	installerDownloadGB = 1.7
	installerRequiredGB = 2
)

// SetupPlan describes what JARVIS needs to become operational.
type SetupPlan struct {
	Info                  *setup.SystemInfo          `json:"info"`
	Recommendation        *setup.ModelRecommendation `json:"recommendation"`
	InstalledModels       []string                   `json:"installed_models"`
	CurrentModel          string                     `json:"current_model"`
	SelectedModel         string                     `json:"selected_model"`
	CurrentModelInstalled bool                       `json:"current_model_installed"`
	RecommendedInstalled  bool                       `json:"recommended_installed"`
	NeedsInstall          bool                       `json:"needs_install"`
	NeedsStart            bool                       `json:"needs_start"`
	NeedsModel            bool                       `json:"needs_model"`
	Ready                 bool                       `json:"ready"`
	InstallerDownloadGB   float64                    `json:"installer_download_gb"`
}

// SetupProgress is emitted as setup:progress while automatic setup runs.
type SetupProgress struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Percent int    `json:"percent"`
	Detail  string `json:"detail,omitempty"`
}

// GetSetupPlan inspects the machine, installed models and the active config.
func (a *App) GetSetupPlan() (*SetupPlan, error) {
	info, err := a.DetectSystem()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, _ := a.ollama.ListModels(ctx)
	if models == nil {
		models = []string{}
	}

	recommendation := setup.RecommendForSystem(info)
	if recommendation == nil {
		return nil, fmt.Errorf("could not recommend a model for this system")
	}

	current := a.ollama.Model()
	currentInstalled := hasModel(models, current)
	recommendedInstalled := hasModel(models, recommendation.Model)
	selected := recommendation.Model
	if currentInstalled {
		selected = current
	}

	plan := &SetupPlan{
		Info:                  info,
		Recommendation:        recommendation,
		InstalledModels:       models,
		CurrentModel:          current,
		SelectedModel:         selected,
		CurrentModelInstalled: currentInstalled,
		RecommendedInstalled:  recommendedInstalled,
		NeedsInstall:          !info.OllamaInstalled && !info.OllamaRunning,
		NeedsStart:            info.OllamaInstalled && !info.OllamaRunning,
		NeedsModel:            !hasModel(models, selected),
		// The active model must be present for the assistant to work. The
		// recommendation may already be installed while the configured model
		// is missing; that still requires the wizard to switch models.
		Ready:               info.OllamaRunning && currentInstalled,
		InstallerDownloadGB: installerDownloadGB,
	}
	return plan, nil
}

// RunSetup performs the complete one-click setup: install Ollama if needed,
// start it, pull the selected model and save it as the active model.
func (a *App) RunSetup(modelName string) (string, error) {
	a.setupMu.Lock()
	if a.setupRunning {
		a.setupMu.Unlock()
		a.playVoice(voice.KeyError)
		return "", fmt.Errorf("setup is already running")
	}
	a.setupRunning = true
	a.setupMu.Unlock()
	defer func() {
		a.setupMu.Lock()
		a.setupRunning = false
		a.setupMu.Unlock()
	}()
	a.playVoice(voice.KeySetupStart)

	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		plan, err := a.GetSetupPlan()
		if err != nil {
			a.playVoice(voice.KeyError)
			return "", err
		}
		modelName = plan.SelectedModel
	}
	if !modelNamePattern.MatchString(modelName) {
		a.playVoice(voice.KeyError)
		return "", fmt.Errorf("invalid model name")
	}

	base := context.Background()
	if a.ctx != nil {
		base = a.ctx
	}
	ctx, cancel := context.WithTimeout(base, 3*time.Hour)
	defer cancel()

	a.emitSetupProgress("detect", "Scanning hardware...", 1, "")
	info, err := a.DetectSystem()
	if err != nil {
		return a.setupError(err)
	}
	if info.DiskFreeGB > 0 && !info.OllamaInstalled && !info.OllamaRunning && info.DiskFreeGB < installerRequiredGB+5 {
		return a.setupError(fmt.Errorf("not enough free disk space to install Ollama; need at least %d GB", installerRequiredGB+5))
	}

	if !info.OllamaInstalled && !info.OllamaRunning {
		a.playVoice(voice.KeySetupImport)
		a.emitSetupProgress("install", "Installing Ollama...", 2, "Windows Package Manager or the official signed installer")
		err = setup.InstallOllama(ctx, func(progress setup.InstallProgress) {
			a.emitSetupProgress(progress.Stage, progress.Message, progress.Percent, progress.Detail)
		})
		if err != nil {
			return a.setupError(err)
		}
	}

	if !info.OllamaRunning {
		a.emitSetupProgress("start", "Starting Ollama...", 92, "")
		if err := setup.StartOllama(ctx); err != nil {
			return a.setupError(err)
		}
	}

	healthCtx, healthCancel := context.WithTimeout(ctx, 10*time.Second)
	healthErr := a.ollama.Healthy(healthCtx)
	healthCancel()
	if healthErr != nil {
		return a.setupError(fmt.Errorf("Ollama is installed but its API is not responding: %w", healthErr))
	}

	modelsCtx, modelsCancel := context.WithTimeout(ctx, 10*time.Second)
	models, listErr := a.ollama.ListModels(modelsCtx)
	modelsCancel()
	if listErr != nil {
		return a.setupError(fmt.Errorf("could not list installed models: %w", listErr))
	}

	// When Ollama was not running, GetSetupPlan could not see installed
	// models. Prefer an already installed active model (or the recommended
	// one) over downloading the same model again.
	if !hasModel(models, modelName) {
		if current := a.ollama.Model(); current != "" && hasModel(models, current) {
			modelName = current
			a.emitSetupProgress("pull", "Using installed model "+modelName, 100, modelName)
		} else if recommendation := setup.RecommendForSystem(info); recommendation != nil && hasModel(models, recommendation.Model) {
			modelName = recommendation.Model
			a.emitSetupProgress("pull", "Using installed model "+modelName, 100, modelName)
		}
	}

	if !hasModel(models, modelName) {
		if info.DiskFreeGB > 0 {
			// Model weights plus a working margin. Prefer the exact catalog
			// size; fall back to the recommendation for custom tags.
			needed := 5
			if size := setup.ModelDownloadGB(modelName); size > 0 {
				needed += int(math.Ceil(size))
			} else if plan, planErr := a.GetSetupPlan(); planErr == nil && plan.Recommendation != nil {
				needed += int(math.Ceil(plan.Recommendation.DownloadGB))
			}
			if info.DiskFreeGB < needed {
				return a.setupError(fmt.Errorf("not enough free disk space for %s; need about %d GB", modelName, needed))
			}
		}

		a.emitSetupProgress("pull", "Downloading "+modelName+"...", 0, modelName)
		lastPercent := -1
		lastEmit := time.Time{}
		err = a.ollama.PullModelWithProgress(ctx, modelName, func(progress ollama.PullProgress) error {
			if progress.Total > 0 {
				if progress.Percent == lastPercent && time.Since(lastEmit) < 500*time.Millisecond {
					return nil
				}
				lastPercent = progress.Percent
				lastEmit = time.Now()
			} else if time.Since(lastEmit) < 500*time.Millisecond {
				return nil
			} else {
				lastEmit = time.Now()
			}
			message := strings.TrimSpace(progress.Status)
			if message == "" {
				message = "Downloading " + modelName + "..."
			}
			detail := modelName
			if progress.Total > 0 {
				detail = fmt.Sprintf("%s · %s / %s", modelName, formatProgressBytes(progress.Completed), formatProgressBytes(progress.Total))
			}
			a.emitSetupProgress("pull", message, progress.Percent, detail)
			return nil
		})
		if err != nil {
			return a.setupError(fmt.Errorf("model download failed: %w", err))
		}

		verifyCtx, verifyCancel := context.WithTimeout(ctx, 10*time.Second)
		models, listErr = a.ollama.ListModels(verifyCtx)
		verifyCancel()
		if listErr != nil || !hasModel(models, modelName) {
			return a.setupError(fmt.Errorf("model download finished but %s is not installed", modelName))
		}
	}

	a.emitSetupProgress("save", "Saving model selection...", 99, modelName)
	if err := a.saveModelSelection(modelName); err != nil {
		return a.setupError(err)
	}
	a.emitSetupProgress("done", "JARVIS is ready.", 100, modelName)
	a.playVoice(voice.KeySetupReady)
	return fmt.Sprintf("Ollama is running and %s is ready.", modelName), nil
}

func (a *App) saveModelSelection(modelName string) error {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()

	a.stateMu.Lock()
	settings := a.settings
	settings.Model = modelName
	a.stateMu.Unlock()

	if err := config.Save(configPath(), settings); err != nil {
		return fmt.Errorf("model is installed, but settings could not be saved: %w", err)
	}

	a.ollama.SetModel(modelName)
	a.stateMu.Lock()
	a.settings.Model = modelName
	a.stateMu.Unlock()
	return nil
}

func (a *App) emitSetupProgress(stage, message string, percent int, detail string) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	a.emitEvent(eventSetupProgress, SetupProgress{
		Stage:   stage,
		Message: message,
		Percent: percent,
		Detail:  detail,
	})
}

func (a *App) setupError(err error) (string, error) {
	a.playVoice(voice.KeyError)
	a.emitSetupProgress("error", err.Error(), 0, "")
	return "", err
}

func hasModel(models []string, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, model := range models {
		if modelNamesEqual(model, name) {
			return true
		}
	}
	return false
}

func modelNamesEqual(left, right string) bool {
	normalize := func(value string) string {
		value = strings.TrimSpace(value)
		return strings.TrimSuffix(value, ":latest")
	}
	return normalize(left) == normalize(right)
}

func formatProgressBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.2f GB", float64(bytes)/gb)
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mb)
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/kb)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
