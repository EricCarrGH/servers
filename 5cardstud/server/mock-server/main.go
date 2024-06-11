package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
)

// This started as a sync.Map but could revert back to a map since a keyed mutex is being used
// to restrict state reading/setting to one thread at a time
var stateMap sync.Map
var tables []GameTable = []GameTable{}

var tableMutex KeyedMutex

type KeyedMutex struct {
	mutexes sync.Map // Zero value is empty and ready for use
}

func (m *KeyedMutex) Lock(key string) func() {
	key = strings.ToLower(key)
	value, _ := m.mutexes.LoadOrStore(key, &sync.Mutex{})
	mtx := value.(*sync.Mutex)
	mtx.Lock()
	return func() { mtx.Unlock() }
}

func main() {
	log.Print("Starting server...")

	// Local dev mode - do not update live lobby
	UpdateLobby = os.Getenv("GO_LOCAL") != "1" && os.Getenv("USER") != "eric"

	if !UpdateLobby {
		log.Printf("Running in LOCAL MODE. Not updating Lobby")
	}

	// Determine port for HTTP service.
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Listing on port %s", port)

	router := gin.Default()

	router.GET("/view", apiView)

	router.GET("/state", apiState)
	router.POST("/state", apiState)

	router.GET("/move/:move", apiMove)
	router.POST("/move/:move", apiMove)

	router.GET("/leave", apiLeave)
	router.POST("/leave", apiLeave)

	router.GET("/tables", apiTables)
	router.GET("/updateLobby", apiUpdateLobby)

	//	router.GET("/REFRESHLOBBY", apiRefresh)

	initializeGameServer()
	initializeRealTables()

	router.Run(":" + port)
}

// Serializes the results, either as json (default), or raw (close to FujiNet json parsing result)
// raw=1 -  or as key[char 0]value[char 0] pairs
// - fc=U/L - (may use with raw) force data case all upper or lower

func serializeResults(c *gin.Context, obj any) {
	if c.Query("raw") == "1" {
		lineDelimiter := "\u0000"
		if c.Query("lf") == "1" {
			lineDelimiter = "\n"
		}
		jsonBytes, _ := json.Marshal(obj)
		jsonResult := string(jsonBytes)

		// Strip out [,],{,}
		jsonResult = strings.ReplaceAll(jsonResult, "{", "")
		jsonResult = strings.ReplaceAll(jsonResult, "}", "")
		jsonResult = strings.ReplaceAll(jsonResult, "[", "")
		jsonResult = strings.ReplaceAll(jsonResult, "]", "")

		// Convert : to new line
		jsonResult = strings.ReplaceAll(jsonResult, ":", lineDelimiter)

		// Convert commas to new line
		jsonResult = strings.ReplaceAll(jsonResult, "\",", lineDelimiter)
		jsonResult = strings.ReplaceAll(jsonResult, ",\"", lineDelimiter)
		jsonResult = strings.ReplaceAll(jsonResult, "\"", "")

		if c.Query("uc") == "1" {
			jsonResult = strings.ToUpper(jsonResult)
		}

		if c.Query("lc") == "1" {
			jsonResult = strings.ToLower(jsonResult)
		}

		c.String(http.StatusOK, jsonResult)

	} else {
		c.JSON(http.StatusOK, obj)
	}
}

// Api Request steps
// 1. Get state
// 2. Game Logic
// 3. Save state
// 4. Return client centric state

// Executes a move for the client player, if that player is currently active
func apiMove(c *gin.Context) {

	state, unlock := getState(c, 0)
	func() {
		defer unlock()

		// Access check - only move if the client is the active player
		if state.clientPlayer == state.ActivePlayer {
			move := strings.ToUpper(c.Param("move"))
			state.performMove(move)
			saveState(state)
			state = state.createClientState()
		}
	}()

	serializeResults(c, state)
}

// Steps forward in the emulated game and returns the updated state
func apiState(c *gin.Context) {
	playerCount, _ := strconv.Atoi(c.DefaultQuery("count", "0"))
	hash := c.Query("hash")
	state, unlock := getState(c, playerCount)

	func() {
		defer unlock()
		if state.clientPlayer >= 0 {
			state.runGameLogic()
			saveState(state)
		}
		state = state.createClientState()
	}()

	// Check if passed in hash matches the state
	if len(hash) > 0 && hash == state.hash {
		serializeResults(c, "1")
		return
	}

	serializeResults(c, state)
}

// Drop from the specified table
func apiLeave(c *gin.Context) {
	state, unlock := getState(c, 0)

	func() {
		defer unlock()

		if state.clientPlayer >= 0 {
			state.clientLeave()
			state.updateLobby()
			saveState(state)
		}
	}()
	serializeResults(c, "bye")
}

// Returns a view of the current state without causing it to change. For debugging side-by-side with a client
func apiView(c *gin.Context) {

	state, unlock := getState(c, 0)
	func() {
		defer unlock()

		state = state.createClientState()
	}()

	c.IndentedJSON(http.StatusOK, state)
}

// Returns a list of real tables with player/slots for the client
func apiTables(c *gin.Context) {
	tableOutput := []GameTable{}
	for _, table := range tables {
		value, ok := stateMap.Load(table.Table)
		if ok {
			state := value.(*GameState)
			if state.registerLobby {
				humanPlayerSlots, humanPlayerCount := state.getHumanPlayerCountInfo()
				table.CurPlayers = humanPlayerCount
				table.MaxPlayers = humanPlayerSlots
				tableOutput = append(tableOutput, table)
			}
		}
	}
	serializeResults(c, tableOutput)
	//c.JSON(http.StatusOK, tableOutput)
}

// Forces an update of all tables to the lobby - useful for adhoc use if the Lobby restarts or loses info
func apiUpdateLobby(c *gin.Context) {
	for _, table := range tables {
		value, ok := stateMap.Load(table.Table)
		if ok {
			state := value.(*GameState)
			state.updateLobby()
		}
	}

	serializeResults(c, "Lobby Updated")
}

// Gets the current game state for the specified table and adds the player id of the client to it
func getState(c *gin.Context, playerCount int) (*GameState, func()) {
	table := c.Query("table")

	if table == "" {
		table = "default"
	}
	table = strings.ToLower(table)
	player := c.Query("player")

	// Lock by the table so to avoid multiple threads updating the same table state
	unlock := tableMutex.Lock(table)

	return getTableState(table, player, playerCount), unlock
}

func getTableState(table string, playerName string, playerCount int) *GameState {
	value, ok := stateMap.Load(table)

	var state *GameState

	if ok {
		stateCopy := *value.(*GameState)
		state = &stateCopy

		// Update player count for table if changed
		if state.isMockGame && playerCount > 1 && playerCount < 9 && playerCount != len(state.Players) {
			if len(state.Players) > playerCount {
				state = createGameState(playerCount, true, false)
				state.table = table
			} else {
				state.updateMockPlayerCount(playerCount)
			}
		}
	} else {
		// Create a brand new game
		state = createGameState(playerCount, true, false)
		state.table = table
		state.updateLobby()
	}

	//player := c.Query("player")
	if state.isMockGame {
		state.clientPlayer = 0
	} else {
		state.setClientPlayerByName(playerName)
	}
	return state
}

func saveState(state *GameState) {
	stateMap.Store(state.table, state)
}

func initializeRealTables() {

	// Create the real servers (hard coded for now)
	createRealTable("The Basement", "basement", 0, true)
	createRealTable("The Den", "den", 0, true)
	createRealTable("AI Room - 2 bots", "ai2", 2, true)
	createRealTable("AI Room - 4 bots", "ai4", 4, true)
	createRealTable("AI Room - 6 bots", "ai6", 6, true)

	for i := 1; i < 8; i++ {
		createRealTable(fmt.Sprintf("Dev Room - %d bots", i), fmt.Sprintf("dev%d", i), i, false)
	}

}

func createRealTable(serverName string, table string, botCount int, registerLobby bool) {
	state := createGameState(botCount, false, registerLobby)
	state.table = table
	state.serverName = serverName
	saveState(state)
	state.updateLobby()

	tables = append([]GameTable{{Table: table, Name: serverName}}, tables...)

	if UpdateLobby {
		time.Sleep(time.Millisecond * time.Duration(100))
	}
}
