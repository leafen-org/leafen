/**
 * How a message reaches the person. Without a tone it is progress or a
 * routine save and is not shown, as Notion shows none; "default" or
 * "success" is the result of something they did, a toast that goes by
 * itself; "error" is a failure or something to do again, a toast that stays
 * a little longer and can be closed. Connection trouble is the sync badge's,
 * not a message.
 */
export type ProductStatusTone = "default" | "success" | "error";

export type ProductStatusSink = (message: string, tone?: ProductStatusTone) => void;
