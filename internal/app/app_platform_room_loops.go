//go:build !leafen_noapps

package app

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Room loops (docs/app-platform.md 12): an app whose realtime worker is a
// loop — a game's referee — gets one run of it for each room while people
// are in it, started when the first joins and told to stop when the last
// leaves or the app stops. The room's messages to the server wait in the
// loop's queue for leafen_room_next, which also serves as its clock, and
// leafen_room_send goes to everyone in the room. A loop is a long-running
// run (an hour at most, started again while the room is occupied); with no
// room for another loop on the server, a room's messages to the server go
// unanswered.

const (
	appRoomLoopsMax      = 32
	appRoomLoopQueue     = 256
	appRoomLoopBatch     = 64
	appRoomLoopWaitMax   = 30 * time.Second
	appRoomLoopSendBytes = 64 << 10
	appRoomLoopRestart   = 5 * time.Second
)

type appRoomLoop struct {
	name, installID, tenancy, room string
	queue                          chan appRoomMessage
	run                            *appLongRun
	ended                          bool
}

var appRoomLoops = struct {
	sync.Mutex
	loops map[string]*appRoomLoop
}{loops: map[string]*appRoomLoop{}}

// startAppRoomLoop begins a room's loop when its app has one and the
// server has room for it.
func (rooms *appRooms) startAppRoomLoop(app core.App, install storedAppInstall, manifest appManifest, name string, grant appRoomGrant) {
	if manifest.Realtime == nil || !manifest.Realtime.Loop || appWorkerLimits.longRuns == 0 {
		return
	}
	appRoomLoops.Lock()
	if _, running := appRoomLoops.loops[name]; running || len(appRoomLoops.loops) >= appRoomLoopsMax {
		appRoomLoops.Unlock()
		return
	}
	loop := &appRoomLoop{name: name, installID: install.ID, tenancy: grant.Tenancy, room: grant.Room, queue: make(chan appRoomMessage, appRoomLoopQueue)}
	appRoomLoops.loops[name] = loop
	appRoomLoops.Unlock()
	go rooms.runAppRoomLoop(app, install, manifest, loop)
}

// runAppRoomLoop runs the loop again while its room stays occupied.
func (rooms *appRooms) runAppRoomLoop(app core.App, install storedAppInstall, manifest appManifest, loop *appRoomLoop) {
	defer func() {
		appRoomLoops.Lock()
		delete(appRoomLoops.loops, loop.name)
		appRoomLoops.Unlock()
	}()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), appLongRunLifetime)
		run := &appLongRun{installID: install.ID, tenancy: loop.tenancy, worker: manifest.Realtime.Worker, function: manifest.Realtime.Function,
			running: true, started: time.Now(), cancel: cancel, stopSignal: make(chan struct{})}
		appRoomLoops.Lock()
		if loop.ended {
			appRoomLoops.Unlock()
			cancel()
			return
		}
		loop.run = run
		appRoomLoops.Unlock()
		input, _ := json.Marshal(map[string]any{"room": loop.room, "tenancy": loop.tenancy})
		_, _, err := runAppWorkerWith(ctx, app, install, manifest, appRun{Trigger: "roomLoop", Ref: loop.room, Worker: run.worker,
			Function: run.function, Input: input, Tenancy: loop.tenancy}, run, loop)
		cancel()
		appRoomLoops.Lock()
		ended := loop.ended
		appRoomLoops.Unlock()
		if ended {
			return
		}
		if err != nil {
			app.Logger().Warn("leafen app room loop failed", "install", install.ID, "room", loop.room, "error", err)
		}
		time.Sleep(appRoomLoopRestart)
	}
}

// endAppRoomLoop tells a room's loop to stop once the room empties.
func endAppRoomLoop(name string) {
	appRoomLoops.Lock()
	defer appRoomLoops.Unlock()
	loop := appRoomLoops.loops[name]
	if loop == nil || loop.ended {
		return
	}
	loop.ended = true
	if loop.run != nil {
		loop.run.stop()
	}
}

// queueAppRoomLoop hands a room's message to its loop; false when the room
// has none.
func queueAppRoomLoop(name string, message appRoomMessage) bool {
	appRoomLoops.Lock()
	loop := appRoomLoops.loops[name]
	appRoomLoops.Unlock()
	if loop == nil {
		return false
	}
	select {
	case loop.queue <- message:
	default:
		// A loop that falls this far behind loses the newest messages.
	}
	return true
}

// roomNext answers leafen_room_next {waitMs}: the messages waiting, up to
// 64, after waiting up to the given time for the first, with who sent each;
// whether to stop; and how many are in the room.
func (host *appWorkerHost) roomNext(payload []byte) ([]byte, error) {
	if host.roomLoop == nil {
		return nil, errors.New("leafen_room_next is for room loops")
	}
	request := struct {
		WaitMs int64 `json:"waitMs"`
	}{}
	_ = json.Unmarshal(payload, &request)
	pause := min(max(time.Duration(request.WaitMs)*time.Millisecond, 0), appRoomLoopWaitMax)
	messages, senders := []string{}, []appRoomSender{}
	timer := time.NewTimer(pause)
	defer timer.Stop()
	select {
	case message := <-host.roomLoop.queue:
		messages, senders = append(messages, message.text), append(senders, message.from)
	case <-timer.C:
	case <-host.long.stopSignal:
	case <-host.ctx.Done():
	}
drain:
	for len(messages) < appRoomLoopBatch {
		select {
		case message := <-host.roomLoop.queue:
			messages, senders = append(messages, message.text), append(senders, message.from)
		default:
			break drain
		}
	}
	peers := 0
	if appRoomRegistry != nil {
		appRoomRegistry.mu.Lock()
		peers = appRoomRegistry.peers[host.roomLoop.name]
		appRoomRegistry.mu.Unlock()
	}
	stop := host.long.stopping.Load() || host.ctx.Err() != nil
	if deadline, ok := host.ctx.Deadline(); ok && time.Until(deadline) < appLongRunWaitMax {
		stop = true
	}
	// Each message's sender, as the server knows them, at the same place in senders.
	return json.Marshal(map[string]any{"messages": messages, "senders": senders, "stop": stop, "peers": peers})
}

// roomSend answers leafen_room_send {message}: to everyone in the room.
func (host *appWorkerHost) roomSend(payload []byte) ([]byte, error) {
	if host.roomLoop == nil {
		return nil, errors.New("leafen_room_send is for room loops")
	}
	request := struct {
		Message string `json:"message"`
	}{}
	if err := json.Unmarshal(payload, &request); err != nil || request.Message == "" || len(request.Message) > appRoomLoopSendBytes {
		return json.Marshal(map[string]string{"error": "a message is text of up to 64 KB"})
	}
	if appRoomRegistry == nil || appRoomRegistry.server.BroadcastStateless(host.roomLoop.name, request.Message) != nil {
		return json.Marshal(map[string]string{"error": "the room is closed"})
	}
	return json.Marshal(map[string]bool{"ok": true})
}
