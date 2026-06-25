package atrinis

import "github.com/fujiwarashigure/ATRI-X/internal/atriena"

const (
	ModeRecord = atriena.ModeRecord
	ModeDirect = atriena.ModeDirect
	ModeLLM    = atriena.ModeLLM

	StatusAccepted    = atriena.StatusAccepted
	StatusQueued      = atriena.StatusQueued
	StatusRunning     = atriena.StatusRunning
	StatusCompleted   = atriena.StatusCompleted
	StatusFailed      = atriena.StatusFailed
	StatusDuplicate   = atriena.StatusDuplicate
	StatusUnsupported = atriena.StatusUnsupported
)

type Request = atriena.Request
type Atriwisp = atriena.Atriwisp
type SegmentKind = atriena.SegmentKind
type Segment = atriena.Segment
type Target = atriena.Target
type Response = atriena.Response
type Call = atriena.Call
type Event = atriena.Event

const (
	SegmentKindText  = atriena.SegmentKindText
	SegmentKindImage = atriena.SegmentKindImage
	SegmentKindFile  = atriena.SegmentKindFile
)
