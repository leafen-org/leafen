export type DateMentionChange = {
  date: string;
  /** Where the date ends; null takes the end away, absent leaves it as it is. */
  dateEnd?: string | null;
  dateTimeZone: string | null;
  dateReminder: string | null;
  /** How the mention shows its date; left as it is when absent. */
  dateFormat?: string;
  /** How the mention shows its time; null shows it in 12 hours, absent leaves it as it is. */
  dateTimeFormat?: string | null;
};
