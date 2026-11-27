package app

// People ask for what they can't do themselves, as Notion's requests: access
// to a page they can't open or edit, a member or a guest added for them. The
// organization's sharing security settings gain who may invite guests to
// pages, who may add members (owners, or any member with an owner's
// approval) and whether people outside a workspace may ask for its pages,
// each as it works today until changed. A request waits in one table until
// someone who may grant it decides, and a page's request reaches that page's
// managers' inbox as a notification of its own kind.

const accessRequestsTable = "leafen_access_requests"
