import { describe, expect, it } from "vitest";

import {
  getDefaultImagePreviewPrice,
  getImagePricePlaceholder,
  imagePricingPlatforms,
  imagePricingI18nKey,
  supportsImagePricingPlatform,
} from "../groupsImagePricing";

describe("groups image pricing platform support", () => {
  it("includes OpenAI image groups", () => {
    expect(supportsImagePricingPlatform("openai")).toBe(true);
    expect(imagePricingPlatforms.has("openai")).toBe(true);
  });

  it("keeps non-media group platforms out of the image pricing controls", () => {
    expect(supportsImagePricingPlatform("anthropic")).toBe(false);
  });

  it("includes Composite groups in image pricing controls", () => {
    expect(supportsImagePricingPlatform("composite")).toBe(true);
    expect(imagePricingPlatforms.has("composite")).toBe(true);
  });

  it("uses the image pricing copy", () => {
    expect(imagePricingI18nKey("openai", "title")).toBe(
      "admin.groups.imagePricing.title",
    );
  });

  it("uses the generic image price defaults", () => {
    expect(getImagePricePlaceholder("openai", "image_price_1k")).toBe("0.134");
    expect(getDefaultImagePreviewPrice("openai", "image_price_2k")).toBe(0.201);
  });
});
