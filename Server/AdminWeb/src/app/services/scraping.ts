import { TagScrapingUseCases } from "@/features/scraping/application/tag-scraping-use-cases";
import { HttpTagScrapingGateway } from "@/features/scraping/infrastructure/http-tag-scraping-gateway";

const tagScraping = new TagScrapingUseCases(new HttpTagScrapingGateway());

export function useTagScraping(): TagScrapingUseCases {
  return tagScraping;
}
