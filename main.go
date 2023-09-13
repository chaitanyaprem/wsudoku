package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gdamore/tcell/v2"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/multiformats/go-multiaddr"
	"github.com/rivo/tview"
	"github.com/waku-org/go-waku/waku/v2/node"
	"github.com/waku-org/go-waku/waku/v2/peerstore"
	"github.com/waku-org/go-waku/waku/v2/protocol/pb"
	"github.com/waku-org/go-waku/waku/v2/protocol/store"
	"github.com/waku-org/go-waku/waku/v2/utils"
	"go.uber.org/zap"
)

var (
	// exitHandlers contains all functions that need to be called during exit
	exitHandlers []func()
	// timeStart is used to render the game timer
	timeStart = time.Now()
	// version
	version    = "dev"
	wakuNode   *node.WakuNode
	player     Player
	log        *zap.Logger
	cancelFunc context.CancelFunc

	pages = tview.NewPages()

	menuFlex = tview.NewFlex()
	gameFlex = tview.NewFlex()

	menu     = tview.NewList().ShowSecondaryText(false)
	app      = tview.NewApplication()
	infoText = tview.NewTextView()
	gameMenu = tview.NewList()
)

const contentTopicGames = "/wsudoku/1/games/proto"

const contentTopic = "/wsudoku/1/multiplayer/proto"
const pubSubTopic = "/waku/2/wsudoku/"

func init() {
	// seed the RNG or the word auto-selected will remain the same all the time
	rand.Seed(time.Now().UnixNano())

	// cleanup on termination
	c := make(chan os.Signal, 5)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-c
		cleanup()
	}()

	// init other things
	initFlags()
	initKeyboard()
	initHeaderAndFooter()
}

func cleanup() {
	renderMutex.Lock()
	defer renderMutex.Unlock() // unnecessary
	renderEnabled = false
	for _, exitHandler := range exitHandlers {
		exitHandler()
	}
}

func main() {
	defer cleanup()

	gameSetup()

	menu.AddItem("New Game", "", 1, CreateNewGame)
	menu.AddItem("Find Games", "", 2, DisplayAvailableGames)

	infoText.
		SetTitle("Useful info:")

	gameMenu.SetTitle("List of games online:")
	menuFlex.SetDirection(tview.FlexRow).
		AddItem(tview.NewFlex().
			AddItem(menu, 0, 1, true).
			AddItem(gameMenu, 0, 4, false), 0, 6, false).
		AddItem(infoText, 0, 1, false)

		//gameFlex.AddItem(text, 0, 1, false)

	pages.AddPage("Menu", menuFlex, true, true)
	//pages.AddPage("Game", gameFlex, true, true)

	//pages.AddPage("Info", infoText, true, false)

	if err := app.SetRoot(pages, true).EnableMouse(true).Run(); err != nil {
		panic(err)
	}

	menuFlex.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Rune() == 113 {
			cleanup()
			app.Stop()
		}
		return event
	})

	//generateSudoku()

	//play()
}

func DisplayAvailableGames() {
	games, err := FetchGames()
	if err != nil {
		log.Error("Failed to fetch games", zap.Error(err))
		return
	}
	if games == nil || len(games) == 0 {
		log.Info("No games found")
	}
	log.Info("Games Found ", zap.Int("count", len(games)))
	index := 0
	gameMenu.Clear()
	for name, game := range games {
		gameMenu.AddItem(name, fmt.Sprintf("Players Online:%d", len(game.Players)), rune(49+index), JoinGame)
		index++
		log.Info("Game Details ", zap.String("name", name), zap.Int("players", len(game.Players)))
	}
}

func JoinGame() {
	//TODO : Display confirm option to join
	infoText.SetText(infoText.GetText(true) + "\n Joining Game")
}

func CreateNewGame() {
	//TODO: If no peers are connected, then don't let user create a new game
	if len(wakuNode.Host().Network().Peers()) <= 0 {
		log.Error("Cannot create a game as no Peers connected. Wait for peers to be connected")
		return
	}
	//generateSudoku()
	s2 := rand.NewSource(time.Now().Unix())
	title := fmt.Sprintf("Sudoku-%d", rand.New(s2).Uint32())
	payload, err := json.Marshal(NewLiveGame(title, player))
	if err != nil {
		log.Error("Filed to marshal json due to error ", zap.Error(err))
		return
	}
	msg := pb.WakuMessage{ContentTopic: contentTopicGames, Timestamp: utils.GetUnixEpoch(), Payload: payload}

	_, err = wakuNode.Relay().PublishToTopic(context.Background(), &msg, pubSubTopic)
	if err != nil {
		log.Error("Failed to publish new game", zap.Error(err))
		return
	}
	log.Info("Published New game successfully", zap.String("title", title))
}

func gameSetup() {
	createWakuNode()
	player.ID = wakuNode.ID()
}

func createWakuNode() {
	NodeKey, err := crypto.GenerateKey()
	if err != nil {
		fmt.Println("Could not generate random key")
		return
	}

	connNotifier := make(chan node.PeerConnection)
	utils.InitLogger("", "file:./wsudoku.log")
	log = utils.Logger()
	opts := []node.WakuNodeOption{
		node.WithPrivateKey(NodeKey),
		node.WithNTP(),
		//node.WithHostAddress(hostAddr),
		node.WithConnectionNotification(connNotifier),
	}
	opts = append(opts, node.WithWakuRelay())

	wakuNode, err = node.New(opts...)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	go watchPeerConnections(connNotifier)

	maddr, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/60000/p2p/16Uiu2HAmBLMPAxsoGRmEAsSqzLnHE98MFTFsTnah55dJW9fCeySY")

	ctx, cancel := context.WithCancel(context.Background())
	cancelFunc = cancel
	if err := wakuNode.Start(ctx); err != nil {
		fmt.Println(err.Error())
		return
	}
	err = addPeer(wakuNode, &maddr, store.StoreID_v20beta4)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	exitHandlers = append(exitHandlers, WakuCleanup)
}

func watchPeerConnections(connNotifier <-chan node.PeerConnection) {
	for conn := range connNotifier {
		if conn.Connected {
			log.Info(fmt.Sprintf("Peer %s connected", conn.PeerID.Pretty()))
			infoText.SetText(infoText.GetText(true) + "\n Peer connected:" + conn.PeerID.Pretty())
		} else {
			log.Info(fmt.Sprintf("Peer %s disconnected", conn.PeerID.Pretty()))
			infoText.SetText(infoText.GetText(true) + "\n Peer disconnected:" + conn.PeerID.Pretty())
		}
	}
}

func WakuCleanup() {
	defer cancelFunc()
	wakuNode.Stop()
}

func addPeer(wakuNode *node.WakuNode, addr *multiaddr.Multiaddr, protocols ...protocol.ID) error {
	if addr == nil {
		return nil
	}
	_, err := wakuNode.AddPeer(*addr, peerstore.Static, protocols...)
	return err
}
