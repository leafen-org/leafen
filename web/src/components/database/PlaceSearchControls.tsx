import { type ReactNode, useEffect, useRef, useState } from "react";
import { LocationArrowIcon } from "@/icons/leafen";
import { useTranslation } from "react-i18next";
import type { PlaceSearchResponse } from "@/administratorWire.generated";
import type { PlacePropertyValue } from "@/lib/types";
import styles from "./PlacePicker.module.css";
import { fieldEnter } from "@/lib/fieldKeyBoundary";

/** Text leaves the instance only after an explicit search. A closed picker
 * or changed page/session cannot accept a late search or location result.
 * Where an operator's provider searches, its field runs across the window's
 * top and its places follow as rows, as Notion's; the picker's own fields
 * come next, then, with no place yet, "현재 위치" as a menu row. */
export function PlaceSearchControls({ search, enabled, isCurrent, located, onPick, children }: {
 enabled: boolean;
 search?: (query: string, signal: AbortSignal) => Promise<PlaceSearchResponse>;
 isCurrent: () => boolean;
 /** Whether the value has a place; Notion offers "현재 위치" only before it does. */
 located: boolean;
 onPick: (value: PlacePropertyValue) => void;
 children: ReactNode;
}) {
 const {t}=useTranslation("propertyCell");
 const [query,setQuery]=useState("");
 const [result,setResult]=useState<PlaceSearchResponse|null>(null),[error,setError]=useState("");
 const [busy,setBusy]=useState(false);
 const lease=useRef({alive:true,sequence:0,controller:null as AbortController|null});
 useEffect(()=>{const held=lease.current;held.alive=true;
  return()=>{held.alive=false;held.sequence++;held.controller?.abort();};
 },[]);
 function current(sequence:number){return lease.current.alive&&lease.current.sequence===sequence&&isCurrent();}
 async function find(){
  if(!search||!enabled||!query.trim()||busy||!isCurrent())return;
  const sequence=++lease.current.sequence;lease.current.controller?.abort();
  const controller=new AbortController();lease.current.controller=controller;
  setBusy(true);setError("");setResult(null);
  try{const response=await search(query.trim(),controller.signal);if(current(sequence))setResult(response);}
  catch{if(current(sequence)&&!controller.signal.aborted)setError(t("placeSearchFailed"));}
  finally{if(current(sequence))setBusy(false);}
 }
 function locate(){
  if(busy||!isCurrent())return;
  if(!navigator.geolocation){setError(t("placeLocationFailed"));return;}
  const sequence=++lease.current.sequence;lease.current.controller?.abort();setBusy(true);setError("");setResult(null);
  navigator.geolocation.getCurrentPosition(position=>{
   if(!current(sequence))return;
   const {latitude:lat,longitude:lon}=position.coords;
   if(Number.isFinite(lat)&&Number.isFinite(lon)&&Math.abs(lat)<=90&&Math.abs(lon)<=180)onPick({lat,lon,name:null,address:null});
   else setError(t("placeLocationFailed"));setBusy(false);
  },()=>{if(current(sequence)){setError(t("placeLocationFailed"));setBusy(false);}},
  {enableHighAccuracy:false,timeout:10000,maximumAge:0});
 }
 return <>
  {enabled&&search?<>
   <div className={styles.band}><input className={styles.bandInput} aria-label={t("placeSearch")} placeholder={t("placeSearch")} value={query} maxLength={200}
    onChange={event=>{lease.current.sequence++;lease.current.controller?.abort();setBusy(false);setQuery(event.target.value);setResult(null);setError("");}}
    onKeyDown={event=>{if(fieldEnter(event)){event.preventDefault();void find();}}}/>
    <button type="button" className={styles.searchButton} onClick={()=>void find()} disabled={busy||!query.trim()}>{t("placeSearchButton")}</button></div>
   <p className={styles.hint}>{t("placeSearchPrivacy")}</p>
   {result?<><div className={styles.rows} aria-label={t("placeSearchResults")}>
    {result.items.map((item,index)=><button type="button" key={index} className={styles.result} onClick={()=>{if(isCurrent())onPick(item);setResult(null);}}><strong>{item.name||item.address}</strong><span>{item.address}</span></button>)}
    {!result.items.length?<p className={styles.hint}>{t("placeSearchEmpty")}</p>:null}
   </div><a className={styles.hint} href={result.attributionUrl} target="_blank" rel="noopener noreferrer">{result.attribution}</a></>:null}
  </>:null}
  {children}
  {located?null:<div className={styles.rows}>
   <button type="button" className={styles.row} onClick={locate} disabled={busy}>
    <span className={styles.rowIcon} aria-hidden="true"><LocationArrowIcon size="small" /></span>{t("placeCurrentLocation")}</button>
  </div>}
  {busy?<p className={styles.hint} role="status">{t("placeSearching")}</p>:null}
  {error?<p className={styles.hint} role="alert" data-error>{error}</p>:null}
 </>;
}
