import { ReadLogDialogHost } from "@/components/ReadLogDialog";
import { AccountHistoryDialogHost } from "@/components/AccountHistoryDialog";
import { SendItemDialogHost } from "@/components/database/SendItemDialog";
import { AppFileOpeningHost } from "@/components/apps/appFileOpening";
import { DocumentPreviewHost } from "@/components/apps/DocumentPreview";
import { DocumentPresentationHost } from "@/components/apps/DocumentPresentation";

/** The dialogs opened from anywhere in the product, kept while the person moves between pages. */
export function ProductDialogHosts() {
  return <>
    <ReadLogDialogHost />
    <AccountHistoryDialogHost />
    <SendItemDialogHost />
    <AppFileOpeningHost />
    <DocumentPreviewHost />
    <DocumentPresentationHost />
  </>;
}
