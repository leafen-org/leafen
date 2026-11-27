package app

func enforceWorkspaceImportArchiveDLP(
	appDir string,
	source storedWorkspaceImportSource,
	title string,
	scanner organizationDLPContentScanner,
) error {
	if err := scanner.enforce(map[string]any{
		"title":  title,
		"format": workspaceImportFormatNativeArchiveV2,
	}); err != nil {
		return err
	}
	offset := int64(0)
	for index := 0; index < source.ChunkCount; index++ {
		chunk, nextOffset, err := loadWorkspaceImportStagedChunk(appDir, source, offset, index)
		if err != nil {
			return err
		}
		if err := scanner.enforce(chunk.Items); err != nil {
			return err
		}
		offset = nextOffset
	}
	if offset != source.SourceBytes {
		return workspaceImportStateError()
	}
	return nil
}
