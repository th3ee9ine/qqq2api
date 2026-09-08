/**
 * Returns the broad geographic area for an ISO 3166-1 alpha-2 country code.
 *
 * Proxy geolocation responses already contain a country code. Keeping this
 * mapping in the client lets both the IP management table and proxy selector
 * show a useful area (for example, North America or South America) without
 * another request or a schema change.
 */
export type IpArea =
  | 'northAmerica'
  | 'southAmerica'
  | 'europe'
  | 'asia'
  | 'africa'
  | 'oceania'
  | 'antarctica'

const AREA_CODES: Record<IpArea, ReadonlySet<string>> = {
  northAmerica: new Set([
    'AG', 'AI', 'AW', 'BB', 'BL', 'BM', 'BQ', 'BS', 'CA', 'CR', 'CU', 'CW', 'DM', 'DO',
    'GD', 'GL', 'GP', 'GT', 'HN', 'HT', 'JM', 'KN', 'KY', 'LC', 'MF', 'MQ', 'MS', 'MX',
    'NI', 'PA', 'PM', 'PR', 'SV', 'SX', 'TC', 'TT', 'US', 'VC', 'VG', 'VI'
  ]),
  southAmerica: new Set(['AR', 'BO', 'BR', 'CL', 'CO', 'EC', 'FK', 'GF', 'GY', 'PE', 'PY', 'SR', 'UY', 'VE']),
  europe: new Set([
    'AD', 'AL', 'AT', 'AX', 'BA', 'BE', 'BG', 'BY', 'CH', 'CY', 'CZ', 'DE', 'DK', 'EE', 'ES',
    'FI', 'FO', 'FR', 'GB', 'GG', 'GI', 'GR', 'HR', 'HU', 'IE', 'IM', 'IS', 'IT', 'JE', 'LI',
    'LT', 'LU', 'LV', 'MC', 'MD', 'ME', 'MK', 'MT', 'NL', 'NO', 'PL', 'PT', 'RO', 'RS', 'RU',
    'SE', 'SI', 'SJ', 'SK', 'SM', 'UA', 'UK', 'VA', 'XK'
  ]),
  asia: new Set([
    'AE', 'AF', 'AM', 'AZ', 'BD', 'BH', 'BN', 'BT', 'CN', 'GE', 'HK', 'ID', 'IL', 'IN', 'IQ',
    'IR', 'JO', 'JP', 'KG', 'KH', 'KP', 'KR', 'KW', 'KZ', 'LA', 'LB', 'LK', 'MM', 'MN', 'MO',
    'MV', 'MY', 'NP', 'OM', 'PH', 'PK', 'PS', 'QA', 'SA', 'SG', 'SY', 'TH', 'TJ', 'TL', 'TM',
    'TR', 'TW', 'UZ', 'VN', 'YE'
  ]),
  africa: new Set([
    'AO', 'BF', 'BI', 'BJ', 'BW', 'CD', 'CF', 'CG', 'CI', 'CM', 'CV', 'DJ', 'DZ', 'EG', 'EH',
    'ER', 'ET', 'GA', 'GH', 'GM', 'GN', 'GQ', 'GW', 'KE', 'KM', 'LR', 'LS', 'LY', 'MA', 'MG',
    'ML', 'MR', 'MU', 'MW', 'MZ', 'NA', 'NE', 'NG', 'RE', 'RW', 'SC', 'SD', 'SH', 'SL', 'SN',
    'SO', 'SS', 'ST', 'SZ', 'TD', 'TG', 'TN', 'TZ', 'UG', 'YT', 'ZA', 'ZM', 'ZW'
  ]),
  oceania: new Set([
    'AS', 'AU', 'CC', 'CK', 'CX', 'FJ', 'FM', 'GU', 'KI', 'MH', 'MP', 'NC', 'NF', 'NR', 'NU',
    'NZ', 'PF', 'PG', 'PN', 'PW', 'SB', 'TK', 'TO', 'TV', 'UM', 'VU', 'WF', 'WS'
  ]),
  antarctica: new Set(['AQ', 'BV', 'GS', 'HM', 'TF'])
}

export function getIpArea(countryCode?: string | null): IpArea | undefined {
  const code = countryCode?.trim().toUpperCase()
  if (!code) return undefined
  for (const [area, codes] of Object.entries(AREA_CODES) as [IpArea, ReadonlySet<string>][]) {
    if (codes.has(code)) return area
  }
  return undefined
}
