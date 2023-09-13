package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/waku-org/go-waku/waku/v2/protocol/store"
)

type LiveGame struct {
	PlayerCount int      `json:"playerCount"`
	Players     []Player `json:"players,omitEmpty"`
	GameID      string   `json:"gameID"`
	GameData    []byte   `json:"gameData,omitEmpty"`
	Title       string   `json:"title"`
}

type Player struct {
	ID          string `json:"id"`                    //PeerID??
	JoinTime    int64  `json:"joinTime,omitEmpty"`    //time in unixMicro
	CurrentGame string `json:"currentGame,omitEmpty"` //gameID
	IsAdmin     bool   `json:"isAdmin,omitEmpty"`
	Finished    bool   `json:"finished,omitEmpty"`
	//TODO: Other data

}

func NewLiveGame(title string, player Player) LiveGame {

	hash := sha256.Sum256([]byte(title))
	game := LiveGame{PlayerCount: 1, GameID: string(hash[:]), Title: title}
	player.CurrentGame = game.GameID
	player.IsAdmin = true
	player.JoinTime = time.Now().UnixMicro()

	players := make([]Player, 0)
	players = append(players, player)
	game.Players = players

	return game

}

func FetchGames() (map[string]LiveGame, error) {
	query := store.Query{ContentTopics: []string{contentTopicGames}, Topic: pubSubTopic}

	res, err := wakuNode.Store().Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	var game LiveGame
	games := make(map[string]LiveGame)
	for _, msg := range res.Messages {
		err = json.Unmarshal(msg.Payload, &game)
		if err != nil {
			fmt.Println("Failed to decode game message due to error:", err)
		}
		games[game.Title] = game
	}

	return games, nil
}
