export const imagePricingPlatforms = new Set([
  "composite",
  "openai",
]);

export const supportsImagePricingPlatform = (platform: string): boolean =>
  imagePricingPlatforms.has(platform);

export const imagePricingI18nKey = (_platform: string, key: string): string =>
  `admin.groups.imagePricing.${key}`;

type ImagePricingTierKey = "image_price_1k" | "image_price_2k" | "image_price_4k";

const defaultImagePricePlaceholders: Record<
  string,
  Record<ImagePricingTierKey, string>
> = {
  default: {
    image_price_1k: "0.134",
    image_price_2k: "0.201",
    image_price_4k: "0.268",
  },
};

export const getImagePricePlaceholder = (
  platform: string,
  tier: ImagePricingTierKey,
): string => {
  const card = defaultImagePricePlaceholders[platform] ?? defaultImagePricePlaceholders.default;
  return card[tier];
};

export const getDefaultImagePreviewPrice = (
  platform: string,
  tier: ImagePricingTierKey,
): number | null => {
  const placeholder = getImagePricePlaceholder(platform, tier);
  if (placeholder === "") {
    return null;
  }
  const value = Number(placeholder);
  return Number.isFinite(value) ? value : null;
};
