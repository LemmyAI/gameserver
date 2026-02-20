package room

import "errors"

var (
	ErrRoomNotFound = errors.New("room not found")
	ErrRoomFull     = errors.New("room is full")
	ErrNotHost      = errors.New("only host can perform this action")
	ErrNotInRoom    = errors.New("player not in room")
)