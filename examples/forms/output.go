package main

import (
	"context"
	"fmt"
	"time"

	"github.com/go-cli-template/internal/clack/core"
	"github.com/go-cli-template/internal/clack/prompts"
	"github.com/go-cli-template/internal/clack/prompts/banner"
)

func runBanner() error {
	return prompts.Banner("FORMS", prompts.BannerOptions{
		Config: banner.Config{
			Font:     "standard",
			Width:    72,
			Gradient: &banner.GradientOcean,
		},
	})
}

func runAnimatedBanner() error {
	return prompts.AnimatedBanner("FORMS", prompts.BannerOptions{
		Config: banner.Config{
			Font:     "standard",
			Width:    72,
			Gradient: &banner.GradientAurora,
		},
	}, prompts.BannerAnimationOptions{FrameInterval: 75 * time.Millisecond})
}

func runBannerEffects() error {
	for _, example := range []struct {
		text   string
		effect prompts.BannerEffect
	}{
		{text: "WIPE", effect: prompts.BannerEffectWipe},
		{text: "SWEEP", effect: prompts.BannerEffectGradientSweep},
		{text: "FADE", effect: prompts.BannerEffectFade},
	} {
		if err := prompts.AnimatedBanner(example.text, prompts.BannerOptions{
			Config: banner.Config{
				Font:     "standard",
				Width:    72,
				Gradient: &banner.GradientAurora,
			},
		}, prompts.BannerAnimationOptions{
			Effect:        example.effect,
			FrameInterval: 55 * time.Millisecond,
		}); err != nil {
			return err
		}
	}
	return nil
}

func runWorkflowSweepBanner() error {
	var result struct{ Name string }
	return prompts.Workflow(&result).
		WithAnimatedBanner(banner.Config{
			Text:     "WELCOME",
			Font:     "standard",
			Width:    72,
			Gradient: &banner.GradientAurora,
		}, prompts.BannerAnimationOptions{
			Effect:        prompts.BannerEffectGradientSweep,
			FrameInterval: 55 * time.Millisecond,
		}).
		ModelStep("Name", func() core.ComposablePrompt {
			return prompts.TextModel(prompts.TextParams{Message: "Your name?"})
		}).
		RunNative()
}

func runWorkflowAnimatedBanner() error {
	var result struct{ Name string }
	return prompts.Workflow(&result).
		WithAnimatedBanner(banner.Config{
			Text:     "WELCOME",
			Font:     "standard",
			Width:    72,
			Gradient: &banner.GradientOcean,
		}, prompts.BannerAnimationOptions{FrameInterval: 75 * time.Millisecond}).
		ModelStep("Name", func() core.ComposablePrompt {
			return prompts.TextModel(prompts.TextParams{Message: "Your name?"})
		}).
		RunNative()
}

func runMessages() error {
	prompts.Intro("Messages render inline above the next prompt")
	prompts.Message("A plain message uses the vertical go-clack bar.", prompts.MessageOptions{})
	prompts.Info("Info: useful context without interrupting the flow.")
	prompts.Success("Success: the previous step completed.")
	prompts.Warn("Warn: something deserves attention.")
	prompts.Error("Error: an example error line, not a real failure.")
	prompts.Step("Step: the tour is moving to the next form.")
	prompts.Outro("Outro: the messages demo is complete.")
	return nil
}

func runNote() error {
	prompts.Note(
		"Notes are bordered boxes for longer explanations.\nThey keep the same inline styling as the prompts.",
		prompts.NoteOptions{Title: "Inline note"},
	)
	return nil
}

func runSpinner() error {
	spinner := prompts.Spinner(prompts.SpinnerOptions{})
	spinner.Start("Preparing the inline gallery")
	time.Sleep(800 * time.Millisecond)
	spinner.Stop("Spinner finished", 0)
	return nil
}

func runTasks() error {
	return prompts.RunTasks([]prompts.Task{
		{
			Title: "Validate layout",
			ContextTask: func(ctx context.Context, update func(string)) (string, error) {
				for step := 1; step <= 3; step++ {
					select {
					case <-ctx.Done():
						return "", ctx.Err()
					case <-time.After(200 * time.Millisecond):
						update(fmt.Sprintf("Validating layout (%d/3)", step))
					}
				}
				return "Layout validated", nil
			},
		},
		{
			Title: "Check colors",
			ContextTask: func(ctx context.Context, update func(string)) (string, error) {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(300 * time.Millisecond):
					update("Checking colors")
				}
				return "Colors ready", nil
			},
		},
		{
			Title: "Finish up",
			ContextTask: func(ctx context.Context, update func(string)) (string, error) {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(200 * time.Millisecond):
					update("Finishing up")
				}
				return "Gallery ready", nil
			},
		},
	}, prompts.SpinnerOptions{})
}
