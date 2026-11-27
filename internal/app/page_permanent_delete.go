package app

func invalidatePermanentPageRooms(
	roomInvalidator workspaceRoomInvalidator,
	workspaceID string,
) error {
	return roomInvalidator.InvalidateWorkspace(workspaceID)
}

func newPermanentPageDiagnosticReference() string {
	return newDiagnosticReference()
}
