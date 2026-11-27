package app

// An agent on another computer asks to connect without a key
// (mcp_device_connections.go): its request waits ten minutes under a short
// code its person allows in their conversation with an agent already
// connected. The row keeps who asked and from
// where, who allowed or declined it and in which place, and the connection
// it made, as the record of that connection.

const mcpDeviceConnectionsTable = "leafen_mcp_device_connections"
