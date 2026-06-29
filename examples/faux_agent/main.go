package main

import (
	"context"
	"fmt"

	"github.com/detailyang/pig/agent"
	"github.com/detailyang/pig/ai"
)

func main() {
	ai.SetFauxResponses([]ai.AssistantMessage{
		ai.FauxAssistantMessage([]ai.ContentBlock{ai.FauxText("hello from pig")}),
	})
	defer ai.ClearFauxResponses()

	model := ai.Model{ID: "faux", Provider: ai.Provider("faux"), API: ai.ApiFaux}
	runtime := agent.New(agent.Options{
		Model:  model,
		Stream: agent.DefaultStreamFn(),
	})

	state, err := runtime.Run(context.Background(), []agent.Message{
		agent.NewUserMessage("Say hello"),
	})
	if err != nil {
		panic(err)
	}

	for _, message := range state.Messages {
		if message.LLM == nil || message.LLM.Role != ai.RoleAssistant {
			continue
		}
		for _, block := range message.LLM.Content {
			if block.Type == ai.ContentText {
				fmt.Println(block.Text)
			}
		}
	}
}
