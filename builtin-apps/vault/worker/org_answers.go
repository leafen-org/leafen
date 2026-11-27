package main

// A shared vault as Bitwarden's apps and its web vault read it: the
// features it says it has turn the apps' screens on and off. Leafen's
// shared vaults have no plan, seats or billing, so they say they are
// self-hosted, with groups and every collection open to their owners.

// profileOrganization is a member's shared vault as the sync gives it.
func profileOrganization(org organization, m member) map[string]any {
	manage := m.manages()
	return map[string]any{
		"id": org.id(), "name": org.Name, "key": m.Key, "status": m.State, "type": m.Role, "enabled": true,
		"userId": bitwardenID(m.Account), "organizationUserId": m.id(), "identifier": nil, "seats": nil, "maxCollections": nil, "maxStorageGb": nil,
		"usePolicies": false, "useSso": false, "useKeyConnector": false, "useScim": false, "useGroups": true, "useDirectory": false,
		"useEvents": false, "useTotp": true, "use2fa": false, "useApi": false, "useResetPassword": false, "useSecretsManager": false,
		"usePasswordManager": true, "useCustomPermissions": false, "useActivateAutofillPolicy": false, "useRiskInsights": false,
		"usersGetPremium": true, "selfHost": true, "hasPublicAndPrivateKeys": org.PublicKey != "" && org.PrivateKey != "",
		"ssoBound": false, "resetPasswordEnrolled": false, "providerId": nil, "providerName": nil, "providerType": nil,
		"familySponsorshipFriendlyName": nil, "familySponsorshipAvailable": false, "planProductType": 3, "productTierType": 3,
		"keyConnectorEnabled": false, "keyConnectorUrl": nil, "accessSecretsManager": false, "limitCollectionCreation": true,
		"limitCollectionDeletion": true, "limitItemDeletion": false, "allowAdminAccessToAllCollectionItems": true,
		"userIsManagedByOrganization": false, "userIsClaimedByOrganization": false, "isAdminInitiated": false,
		"permissions": permissionsOf(manage), "object": "profileOrganization",
	}
}

func permissionsOf(manage bool) map[string]any {
	permissions := map[string]any{}
	for _, name := range []string{"accessEventLogs", "accessImportExport", "accessReports", "createNewCollections", "editAnyCollection",
		"deleteAnyCollection", "manageGroups", "managePolicies", "manageSso", "manageUsers", "manageResetPassword", "manageScim"} {
		permissions[name] = manage
	}
	return permissions
}

// organizationAnswer is a shared vault as the web vault reads it.
func organizationAnswer(org organization) map[string]any {
	return map[string]any{
		"id": org.id(), "name": org.Name, "businessName": nil, "billingEmail": org.Billing, "identifier": nil,
		"plan": map[string]any{"type": 20, "productTier": 3, "name": "Enterprise (Annually)", "isAnnual": true, "nameLocalizationKey": "planNameEnterprise",
			"descriptionLocalizationKey": "planDescEnterprise", "canBeUsedByBusiness": true, "trialPeriodDays": nil, "hasSelfHost": true,
			"hasPolicies": false, "hasGroups": true, "hasDirectory": false, "hasEvents": false, "hasTotp": true, "has2fa": false, "hasApi": false,
			"hasSso": false, "hasKeyConnector": false, "hasScim": false, "hasResetPassword": false, "usersGetPremium": true,
			"hasCustomPermissions": false, "upgradeSortOrder": 0, "displaySortOrder": 0, "legacyYear": nil, "disabled": false,
			"passwordManager": map[string]any{"baseSeats": 0, "maxSeats": nil, "maxCollections": nil, "maxProjects": nil, "hasAdditionalSeatsOption": false,
				"hasAdditionalStorageOption": false, "hasPremiumAccessOption": false, "basePrice": 0, "seatPrice": 0}, "object": "plan"},
		"planType": 20, "seats": nil, "maxAutoscaleSeats": nil, "maxCollections": nil, "maxStorageGb": nil, "usePolicies": false,
		"useSso": false, "useKeyConnector": false, "useScim": false, "useGroups": true, "useDirectory": false, "useEvents": false,
		"useTotp": true, "use2fa": false, "useApi": false, "useResetPassword": false, "useSecretsManager": false, "usePasswordManager": true,
		"useCustomPermissions": false, "useRiskInsights": false, "selfHost": true, "usersGetPremium": true, "hasPublicAndPrivateKeys": org.PublicKey != "" && org.PrivateKey != "",
		"limitCollectionCreation": true, "limitCollectionDeletion": true, "limitItemDeletion": false, "allowAdminAccessToAllCollectionItems": true,
		"object": "organization",
	}
}

// freePlan is the one plan a shared vault is made with: Leafen's shared
// vaults have no seats, prices or billing.
func freePlan() map[string]any {
	return map[string]any{
		"type": 0, "productTier": 0, "product": 0, "bitwardenProduct": 0, "name": "Free", "isAnnual": false,
		"nameLocalizationKey": "planNameFree", "descriptionLocalizationKey": "planDescFree", "canBeUsedByBusiness": true,
		"trialPeriodDays": nil, "hasSelfHost": true, "hasPolicies": false, "hasMyItems": false, "hasInviteLinks": false, "hasGroups": true,
		"hasDirectory": false, "hasEvents": false, "hasTotp": true, "has2fa": false, "hasApi": false, "hasSso": false,
		"hasResetPassword": false, "usersGetPremium": true, "upgradeSortOrder": 0, "displaySortOrder": 0, "legacyYear": nil,
		"disabled": false, "maxUsers": 0,
		"passwordManager": map[string]any{"baseSeats": 0, "maxSeats": nil, "maxCollections": nil, "maxProjects": nil,
			"hasAdditionalSeatsOption": false, "hasAdditionalStorageOption": false, "hasPremiumAccessOption": false,
			"basePrice": 0, "seatPrice": 0, "additionalStoragePricePerGb": 0, "premiumAccessOptionPrice": 0, "maxAdditionalSeats": nil,
			"baseStorageGb": 0, "allowSeatAutoscale": false, "stripePlanId": nil, "stripeSeatPlanId": nil},
		"secretsManager": nil, "object": "plan",
	}
}
