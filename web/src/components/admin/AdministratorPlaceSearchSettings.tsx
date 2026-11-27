import { useTranslation } from "react-i18next";
import type { AdministratorPlaceSearchSettings, AdministratorPlaceSearchSettingsInput } from "@/administratorWire.generated";
import styles from "./AdministratorSettingsPage.module.css";
import { SettingField, SettingRow, SettingSwitch } from "./AdministratorSettingRows";
import { AppSelect } from "@/components/AppSelect";

/** The operator writes credentials; reads show only their presence. */
export function AdministratorPlaceSearchSettings({value,status,busy,onChange}:{
 value:AdministratorPlaceSearchSettingsInput;status:AdministratorPlaceSearchSettings;busy:boolean;
 onChange:(change:Partial<AdministratorPlaceSearchSettingsInput>)=>void;
}){
 const {t}=useTranslation("serverConsole");
 const disabled=busy||status.environmentConfigured;
 const enabled=status.environmentConfigured||status.environmentDisabled?status.effectiveEnabled:value.enabled;
 const fixed=status.environmentConfigured||status.environmentDisabled;
 // 설정's rows; the provider's fields only once it is on (2026-11-05: a checkbox over boxed fields, a second checkbox to clear).
 return <section className={styles.section} aria-labelledby="administrator-place-search-title" data-place-search-settings>
  <div id="administrator-place-search-title" className={styles.sectionTitle}>{t("placeSearch.title")}</div>
  <div className={styles.settingRows}>
   <SettingSwitch title={t("placeSearch.enabled")} on={enabled} disabled={disabled||status.environmentDisabled}
    description={fixed?t("placeSearch.environment"):t("placeSearch.privacy")} onChange={next=>onChange({enabled:next,clearConfig:false})}/>
   {value.enabled&&!fixed?<>
    <SettingRow title={t("placeSearch.provider")} description={status.configured?t("placeSearch.configured"):undefined}>
     <AppSelect look="setting" aria-label={t("placeSearch.provider")} value={value.provider} disabled={disabled} onChange={event=>onChange({provider:event.target.value,endpoint:"",apiKey:"",apiKeyId:""})}>
      <option value="">{t("placeSearch.choose")}</option><option value="nominatim">Nominatim</option><option value="photon">Photon</option><option value="kakao">Kakao</option><option value="naver">NAVER</option>
     </AppSelect>
    </SettingRow>
    {value.provider?<>
     <SettingField title={t("placeSearch.endpoint")} description={t("placeSearch.manual")}><input type="password" autoComplete="off" value={value.endpoint} disabled={disabled} placeholder={status.configured?t("placeSearch.keepStored"):""} onChange={event=>onChange({endpoint:event.target.value})}/></SettingField>
     {value.provider==="kakao"||value.provider==="naver"?<SettingField title={t("placeSearch.apiKey")}><input type="password" autoComplete="new-password" value={value.apiKey} disabled={disabled} placeholder={status.configured?t("placeSearch.keepStored"):""} onChange={event=>onChange({apiKey:event.target.value})}/></SettingField>:null}
     {value.provider==="naver"?<SettingField title={t("placeSearch.apiKeyId")}><input type="password" autoComplete="off" value={value.apiKeyId} disabled={disabled} placeholder={status.configured?t("placeSearch.keepStored"):""} onChange={event=>onChange({apiKeyId:event.target.value})}/></SettingField>:null}
    </>:null}
   </>:null}
   {status.configured&&!fixed?<SettingSwitch title={t("placeSearch.clear")} on={value.clearConfig} disabled={disabled}
    onChange={next=>onChange({clearConfig:next,enabled:next?false:value.enabled})}/>:null}
  </div>
 </section>;
}
